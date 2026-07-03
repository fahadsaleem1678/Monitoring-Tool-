package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr             string
	AllowOrigin          string
	DatabaseURL          string
	PrometheusURL        string
	PrometheusTimeout    time.Duration
	PrometheusSmokeQuery string
	LogLevel             slog.Level
}

func Load() Config {
	return Config{
		HTTPAddr:             env("HTTP_ADDR", ":8080"),
		AllowOrigin:          env("ALLOW_ORIGIN", "http://localhost:5173"),
		DatabaseURL:          env("DATABASE_URL", "postgres://monitoring:monitoring@localhost:5432/monitoring?sslmode=disable"),
		PrometheusURL:        trimRightSlash(env("PROMETHEUS_URL", "http://localhost:9090")),
		PrometheusTimeout:    envDurationSeconds("PROMETHEUS_TIMEOUT_SECONDS", 10),
		PrometheusSmokeQuery: env("PROMETHEUS_SMOKE_QUERY", "up"),
		LogLevel:             envLogLevel("LOG_LEVEL", slog.LevelInfo),
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envDurationSeconds(key string, fallback int) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return time.Duration(fallback) * time.Second
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func envLogLevel(key string, fallback slog.Level) slog.Level {
	switch strings.ToLower(env(key, "")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return fallback
	}
}

func trimRightSlash(value string) string {
	return strings.TrimRight(value, "/")
}
