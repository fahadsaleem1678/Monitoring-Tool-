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

	"monitoring-tool/backend/internal/alerting"
	"monitoring-tool/backend/internal/auth"
	"monitoring-tool/backend/internal/config"
	"monitoring-tool/backend/internal/health"
	"monitoring-tool/backend/internal/httpapi"
	"monitoring-tool/backend/internal/live"
	"monitoring-tool/backend/internal/notify"
	promclient "monitoring-tool/backend/internal/prometheus"
	"monitoring-tool/backend/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	appStore, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer appStore.Close()

	if err := appStore.Migrate(context.Background()); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	if err := appStore.EnsureAdmin(context.Background(), cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("admin seed failed", "error", err)
		os.Exit(1)
	}
	if err := appStore.EnsureUser(context.Background(), cfg.ViewerUsername, cfg.ViewerPassword, "viewer"); err != nil {
		logger.Error("viewer seed failed", "error", err)
		os.Exit(1)
	}

	prom := promclient.New(cfg.PrometheusURL, cfg.PrometheusTimeout)
	authService := auth.NewService(cfg.JWTSecret)
	healthHandler := health.NewHandler(cfg, prom)
	authHandler := httpapi.NewAuthHandler(appStore, authService)
	metricsHandler := httpapi.NewMetricsHandler(prom)
	dashboardHandler := httpapi.NewDashboardHandler(appStore, prom)
	liveManager := live.NewManager(prom, authService, logger)
	slackNotifier := notify.NewSlackNotifier(cfg.SlackWebhookURL)
	alertHandler := httpapi.NewAlertHandler(appStore, slackNotifier)
	alertEvaluator := alerting.NewEvaluator(appStore, prom, slackNotifier, logger, cfg.AlertEvalInterval)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler.Healthz)
	mux.HandleFunc("GET /readyz", healthHandler.Readyz)
	mux.HandleFunc("GET /api/v1/health", healthHandler.Healthz)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	mux.Handle("POST /api/v1/auth/logout", authService.Middleware(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("GET /api/v1/auth/me", authService.Middleware(http.HandlerFunc(authHandler.Me)))
	mux.HandleFunc("GET /api/v1/metrics/prometheus-smoke", prometheusSmokeHandler(prom, cfg.PrometheusSmokeQuery))
	mux.HandleFunc("GET /api/v1/metrics/query", metricsHandler.Query)
	mux.HandleFunc("GET /api/v1/metrics/query-range", metricsHandler.QueryRange)
	mux.HandleFunc("GET /api/v1/metrics/labels", metricsHandler.Labels)
	mux.HandleFunc("GET /api/v1/metrics/label-values", metricsHandler.LabelValues)
	mux.HandleFunc("GET /api/v1/metrics/series", metricsHandler.Series)
	mux.Handle("GET /api/v1/dashboards", authService.Middleware(http.HandlerFunc(dashboardHandler.List)))
	mux.Handle("POST /api/v1/dashboards", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.Create))))
	mux.Handle("GET /api/v1/dashboards/{id}", authService.Middleware(http.HandlerFunc(dashboardHandler.ByID)))
	mux.Handle("PUT /api/v1/dashboards/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.Update))))
	mux.Handle("DELETE /api/v1/dashboards/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.Delete))))
	mux.Handle("POST /api/v1/dashboards/{id}/panels", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.CreatePanel))))
	mux.Handle("PUT /api/v1/panels/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.UpdatePanel))))
	mux.Handle("DELETE /api/v1/panels/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.DeletePanel))))
	mux.Handle("POST /api/v1/panels/preview", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(dashboardHandler.PreviewPanel))))
	mux.Handle("GET /api/v1/alerts/rules", authService.Middleware(http.HandlerFunc(alertHandler.ListRules)))
	mux.Handle("POST /api/v1/alerts/rules", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(alertHandler.CreateRule))))
	mux.Handle("PUT /api/v1/alerts/rules/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(alertHandler.UpdateRule))))
	mux.Handle("DELETE /api/v1/alerts/rules/{id}", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(alertHandler.DeleteRule))))
	mux.Handle("GET /api/v1/alerts/events", authService.Middleware(http.HandlerFunc(alertHandler.ListEvents)))
	mux.Handle("POST /api/v1/alerts/test-notification", authService.Middleware(auth.RequireAdmin(http.HandlerFunc(alertHandler.TestNotification))))
	mux.Handle("GET /ws/live", liveManager)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withCORS(withRequestLog(logger, mux), cfg.AllowOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go alertEvaluator.Run(ctx)

	go func() {
		logger.Info("backend starting", "addr", cfg.HTTPAddr, "prometheus_url", cfg.PrometheusURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("backend stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

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
