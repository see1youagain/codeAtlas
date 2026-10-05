package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lizhao/codeatlas/internal/agent"
	"github.com/lizhao/codeatlas/internal/config"
	"github.com/lizhao/codeatlas/internal/execution"
	"github.com/lizhao/codeatlas/internal/harness"
	"github.com/lizhao/codeatlas/internal/knowledge"
	"github.com/lizhao/codeatlas/internal/llm"
	"github.com/lizhao/codeatlas/internal/mcpserver"
	"github.com/lizhao/codeatlas/internal/project"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/server"
	"github.com/lizhao/codeatlas/internal/store"
	"github.com/lizhao/codeatlas/internal/tool"
	"github.com/lizhao/codeatlas/internal/worktree"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	workspace, err := repository.New(cfg.RepositoryRoot)
	if err != nil {
		logger.Error("open repository", "error", err)
		os.Exit(1)
	}
	persistence, err := store.Open(cfg.DataPath)
	if err != nil {
		logger.Error("open persistence", "error", err)
		os.Exit(1)
	}
	defer persistence.Close()
	if err := persistence.RecoverInterrupted(context.Background()); err != nil {
		logger.Error("recover interrupted runs", "error", err)
		os.Exit(1)
	}
	profile, err := project.Detect(cfg.RepositoryRoot)
	if err != nil {
		logger.Error("detect project profile", "error", err)
		os.Exit(1)
	}
	worktreeManager, err := worktree.NewManager(cfg.RepositoryRoot, cfg.RuntimeRoot)
	if err != nil {
		logger.Error("initialize worktree manager", "error", err)
		os.Exit(1)
	}
	modelClient := llm.NewOpenAICompatibleClient(cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelName, cfg.RequestTimeout)
	dispatcher := tool.NewDispatcher(workspace)
	react := agent.NewReAct(modelClient, dispatcher, workspace, cfg.MaxIterations)
	planner := agent.NewPlanner(modelClient, workspace, profile)
	executor := execution.New(modelClient, profile, worktreeManager, persistence, cfg.MaxIterations)
	runtime := harness.New(react, planner, executor, workspace, profile, persistence, cfg.EnableWrite)
	knowledgeManager := knowledge.New(workspace, persistence)
	mcpHandlers := mcpserver.New(workspace, runtime, cfg.AdminToken, cfg.EnableWrite)
	api := server.New(workspace, runtime, knowledgeManager, mcpHandlers, cfg.AdminToken, cfg.EnableWrite, logger)

	httpServer := &http.Server{Addr: cfg.Address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 120 * time.Second}
	go func() {
		logger.Info("CodeAtlas started", "address", cfg.Address, "repository", workspace.Info().Name, "revision", workspace.Info().Revision)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
