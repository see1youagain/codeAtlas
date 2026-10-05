package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Address        string
	RepositoryRoot string
	RuntimeRoot    string
	DataPath       string
	EnableWrite    bool
	AdminToken     string
	ModelBaseURL   string
	ModelAPIKey    string
	ModelName      string
	MaxIterations  int
	RequestTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Address:        env("APP_ADDRESS", ":8080"),
		RepositoryRoot: os.Getenv("REPOSITORY_ROOT"),
		RuntimeRoot:    env("RUNTIME_ROOT", "./runtime"),
		DataPath:       env("DATA_PATH", "./runtime/codeatlas.db"),
		EnableWrite:    envBool("ENABLE_WRITE", false),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		ModelBaseURL:   env("MODEL_BASE_URL", "https://api.openai.com/v1"),
		ModelAPIKey:    os.Getenv("MODEL_API_KEY"),
		ModelName:      env("MODEL_NAME", "gpt-4.1-mini"),
		MaxIterations:  envInt("AGENT_MAX_ITERATIONS", 8),
		RequestTimeout: time.Duration(envInt("MODEL_TIMEOUT_SECONDS", 60)) * time.Second,
	}
	if cfg.RepositoryRoot == "" {
		return Config{}, fmt.Errorf("REPOSITORY_ROOT is required")
	}
	if cfg.MaxIterations < 1 || cfg.MaxIterations > 20 {
		return Config{}, fmt.Errorf("AGENT_MAX_ITERATIONS must be between 1 and 20")
	}
	if cfg.EnableWrite && len(cfg.AdminToken) < 16 {
		return Config{}, fmt.Errorf("ADMIN_TOKEN must contain at least 16 characters when ENABLE_WRITE=true")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
