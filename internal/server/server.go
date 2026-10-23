package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lizhao/codeatlas/internal/harness"
	"github.com/lizhao/codeatlas/internal/knowledge"
	"github.com/lizhao/codeatlas/internal/mcpserver"
	"github.com/lizhao/codeatlas/internal/model"
	"github.com/lizhao/codeatlas/internal/repository"
	"github.com/lizhao/codeatlas/internal/store"
)

//go:embed web/dist
var webAssets embed.FS

type Server struct {
	workspace  *repository.Workspace
	harness    *harness.Harness
	knowledge  *knowledge.Manager
	mcp        mcpserver.Handlers
	adminToken string
	write      bool
	logger     *slog.Logger
	sem        chan struct{}
}

func New(workspace *repository.Workspace, runtime *harness.Harness, knowledgeManager *knowledge.Manager, mcpHandlers mcpserver.Handlers, adminToken string, writeEnabled bool, logger *slog.Logger) *Server {
	return &Server{workspace: workspace, harness: runtime, knowledge: knowledgeManager, mcp: mcpHandlers, adminToken: adminToken, write: writeEnabled, logger: logger, sem: make(chan struct{}, 4)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	staticFiles, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/repository", s.repositoryInfo)
	mux.HandleFunc("POST /api/v1/repository/refresh", s.refresh)
	mux.HandleFunc("GET /api/v1/profile", s.profile)
	mux.HandleFunc("GET /api/v1/harness/roles", s.roles)
	mux.HandleFunc("POST /api/v1/ask", s.ask)
	mux.HandleFunc("POST /api/v1/plan", s.plan)
	mux.HandleFunc("GET /api/v1/plans", s.plans)
	mux.HandleFunc("GET /api/v1/plans/{planID}", s.planByID)
	mux.Handle("POST /api/v1/plans/{planID}/approve", s.requireAdmin(http.HandlerFunc(s.approve)))
	mux.Handle("POST /api/v1/plans/{planID}/execute", s.requireAdmin(http.HandlerFunc(s.execute)))
	mux.HandleFunc("GET /api/v1/executions/{executionID}", s.execution)
	mux.Handle("DELETE /api/v1/plans/{planID}/executions/{executionID}", s.requireAdmin(http.HandlerFunc(s.discard)))
	mux.HandleFunc("GET /api/v1/runs/{runID}/events", s.events)
	mux.Handle("POST /api/v1/knowledge/refresh", s.requireAdmin(http.HandlerFunc(s.knowledgeRefresh)))
	mux.HandleFunc("GET /api/v1/knowledge/reports", s.knowledgeReports)
	mux.Handle("GET /api/v1/memories", s.requireMemoryAdmin(http.HandlerFunc(s.memories)))
	mux.Handle("DELETE /api/v1/memories/{memoryID}", s.requireMemoryAdmin(http.HandlerFunc(s.deleteMemory)))
	mux.Handle("DELETE /api/v1/sessions/{sessionID}", s.requireMemoryAdmin(http.HandlerFunc(s.deleteConversation)))
	mux.Handle("POST /mcp", s.mcp.Read)
	mux.Handle("POST /mcp/admin", s.mcp.Admin)
	return s.middleware(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "repository": s.workspace.Info().Name, "write_enabled": s.write, "mcp_protocol": "2026-07-28"})
}

func (s *Server) repositoryInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.workspace.Info())
}
func (s *Server) profile(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.harness.Profile())
}
func (s *Server) roles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.harness.Roles())
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	info, err := s.workspace.Refresh(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	var request model.AskRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.acquire(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, errors.New("server is busy"))
		return
	}
	defer s.release()
	response, err := s.harness.AskWithOptions(r.Context(), requestID(r), request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var request model.PlanRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !s.acquire(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, errors.New("server is busy"))
		return
	}
	defer s.release()
	response, err := s.harness.PlanWithOptions(r.Context(), requestID(r), request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) plans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.harness.Plans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) planByID(w http.ResponseWriter, r *http.Request) {
	plan, err := s.harness.PlanByID(r.Context(), r.PathValue("planID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var request model.ApprovalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.harness.Approve(r.Context(), r.PathValue("planID"), request)
	if err != nil {
		writeStoreOrValidationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	var request model.ExecuteRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.harness.Execute(r.Context(), r.PathValue("planID"), request.ApprovalToken)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "execution": result})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) execution(w http.ResponseWriter, r *http.Request) {
	result, err := s.harness.Execution(r.Context(), r.PathValue("executionID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) discard(w http.ResponseWriter, r *http.Request) {
	if err := s.harness.Discard(r.Context(), r.PathValue("planID"), r.PathValue("executionID")); err != nil {
		writeStoreOrValidationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	events, err := s.harness.Events(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) knowledgeRefresh(w http.ResponseWriter, r *http.Request) {
	report, err := s.knowledge.Refresh(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) knowledgeReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.knowledge.Reports(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.write {
			writeError(w, http.StatusForbidden, errors.New("write workflow is disabled"))
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(provided) != len(s.adminToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.adminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="codeatlas-admin"`)
			writeError(w, http.StatusUnauthorized, errors.New("admin authorization required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Memory administration is independent of code-write ENABLE_WRITE.
func (s *Server) requireMemoryAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if s.adminToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(s.adminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="codeatlas-admin"`)
			writeError(w, http.StatusUnauthorized, errors.New("memory administration requires ADMIN_TOKEN"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) memories(w http.ResponseWriter, r *http.Request) {
	result, err := s.harness.Memories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := s.harness.DeleteMemory(r.Context(), r.PathValue("memoryID")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.harness.DeleteConversation(r.Context(), r.PathValue("sessionID")); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) acquire(ctx context.Context) bool {
	select {
	case s.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}

func (s *Server) release() { <-s.sem }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = fmt.Sprintf("run-%d", time.Now().UnixNano())
		}
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "run_id", id, "duration_ms", time.Since(started).Milliseconds())
	})
}

type requestIDKey struct{}

func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey{}).(string)
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON request: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func writeStoreOrValidationError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusUnprocessableEntity, err)
}
