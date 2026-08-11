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
	JWTSecret            string
	AdminUsername        string
	AdminPassword        string
	ViewerUsername       string
	ViewerPassword       string
	PrometheusURL        string
	PrometheusTimeout    time.Duration
	PrometheusSmokeQuery string
	KubernetesMCPURL     string
	MaxLogLines          int
	AssistantLLMEnabled  bool
	LLMProvider          string
	OpenAIBaseURL        string
	OpenAIModel          string
	OpenAIAPIKey         string
	OpenAITimeout        time.Duration
	OllamaURL            string
	OllamaModel          string
	OllamaTimeout        time.Duration
	AlertEvalInterval    time.Duration
	SlackWebhookURL      string
	AgentServiceToken    string
	LogLevel             slog.Level
}

func Load() Config {
	return Config{
		HTTPAddr:             env("HTTP_ADDR", ":8080"),
		AllowOrigin:          env("ALLOW_ORIGIN", "http://localhost:5173"),
		DatabaseURL:          env("DATABASE_URL", "postgres://monitoring:monitoring@localhost:5432/monitoring?sslmode=disable"),
		JWTSecret:            env("JWT_SECRET", "dev-only-change-me"),
		AdminUsername:        env("ADMIN_USERNAME", "admin"),
		AdminPassword:        env("ADMIN_PASSWORD", "admin123"),
		ViewerUsername:       env("VIEWER_USERNAME", "viewer"),
		ViewerPassword:       env("VIEWER_PASSWORD", "viewer123"),
		PrometheusURL:        trimRightSlash(env("PROMETHEUS_URL", "http://localhost:9090")),
		PrometheusTimeout:    envDurationSeconds("PROMETHEUS_TIMEOUT_SECONDS", 10),
		PrometheusSmokeQuery: env("PROMETHEUS_SMOKE_QUERY", "up"),
		KubernetesMCPURL:     trimRightSlash(env("KUBERNETES_MCP_URL", "http://kubernetes-mcp:8091")),
		MaxLogLines:          envInt("MAX_LOG_LINES", 80),
		AssistantLLMEnabled:  envBool("ASSISTANT_LLM_ENABLED", false),
		LLMProvider:          strings.ToLower(env("LLM_PROVIDER", "ollama")),
		OpenAIBaseURL:        trimRightSlash(env("OPENAI_BASE_URL", "https://api.openai.com/v1")),
		OpenAIModel:          env("OPENAI_MODEL", "gpt-4.1-mini"),
		OpenAIAPIKey:         env("OPENAI_API_KEY", ""),
		OpenAITimeout:        envDurationSeconds("OPENAI_TIMEOUT_SECONDS", 30),
		OllamaURL:            trimRightSlash(env("OLLAMA_URL", "http://host.docker.internal:11434")),
		OllamaModel:          env("OLLAMA_MODEL", "qwen2.5:7b"),
		OllamaTimeout:        envDurationSeconds("OLLAMA_TIMEOUT_SECONDS", 30),
		AlertEvalInterval:    envDurationSeconds("ALERT_EVAL_INTERVAL_SECONDS", 15),
		SlackWebhookURL:      env("SLACK_WEBHOOK_URL", ""),
		AgentServiceToken:    env("AGENT_SERVICE_TOKEN", "dev-agent-token"),
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

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
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
