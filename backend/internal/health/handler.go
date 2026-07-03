package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"monitoring-tool/backend/internal/config"
	promclient "monitoring-tool/backend/internal/prometheus"
)

type Handler struct {
	cfg        config.Config
	prometheus *promclient.Client
	startedAt  time.Time
}

func NewHandler(cfg config.Config, prometheus *promclient.Client) *Handler {
	return &Handler{
		cfg:        cfg,
		prometheus: prometheus,
		startedAt:  time.Now().UTC(),
	}
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "healthy",
		"service":    "monitoring-tool-backend",
		"started_at": h.startedAt.Format(time.RFC3339),
	})
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.prometheus.Timeout())
	defer cancel()

	status := http.StatusOK
	prometheusStatus := "ready"
	if err := h.prometheus.Ready(ctx); err != nil {
		status = http.StatusServiceUnavailable
		prometheusStatus = err.Error()
	}

	writeJSON(w, status, map[string]any{
		"status":         statusText(status),
		"prometheus":     prometheusStatus,
		"prometheus_url": h.cfg.PrometheusURL,
	})
}

func statusText(status int) string {
	if status >= 200 && status < 300 {
		return "ready"
	}
	return "not_ready"
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
