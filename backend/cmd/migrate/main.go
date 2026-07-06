package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"monitoring-tool/backend/internal/config"
	"monitoring-tool/backend/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	appStore, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer appStore.Close()

	if err := appStore.Migrate(ctx); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	if err := appStore.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Error("admin seed failed", "error", err)
		os.Exit(1)
	}
	if err := appStore.EnsureUser(ctx, cfg.ViewerUsername, cfg.ViewerPassword, "viewer"); err != nil {
		logger.Error("viewer seed failed", "error", err)
		os.Exit(1)
	}

	logger.Info("database migrations completed")
}
