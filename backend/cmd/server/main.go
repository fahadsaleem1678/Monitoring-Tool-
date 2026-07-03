package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"monitoring-tool/backend/internal/config"
	"monitoring-tool/backend/internal/health"
	promclient "monitoring-tool/backend/internal/prometheus"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	prom := promclient.New(cfg.PrometheusURL, cfg.PrometheusTimeout)
	healthHandler := health.NewHandler(cfg, prom)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler.Healthz)
	mux.HandleFunc("GET /readyz", healthHandler.Readyz)
	mux.HandleFunc("GET /api/v1/health", healthHandler.Healthz)
	mux.HandleFunc("GET /api/v1/metrics/prometheus-smoke", prometheusSmokeHandler(prom, cfg.PrometheusSmokeQuery))

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withCORS(withRequestLog(logger, mux), cfg.AllowOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("backend starting", "addr", cfg.HTTPAddr, "prometheus_url", cfg.PrometheusURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("backend stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("backend shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("backend stopped")
}

func prometheusSmokeHandler(prom *promclient.Client, query string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), prom.Timeout())
		defer cancel()

		result, err := prom.InstantQuery(ctx, query)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"query":  query,
			"result": result,
		})
	}
}

func withRequestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func withCORS(next http.Handler, allowOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
