package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"monitoring-tool/backend/internal/config"
	promclient "monitoring-tool/backend/internal/prometheus"
)

func main() {
	cfg := config.Load()
	query := cfg.PrometheusSmokeQuery
	if len(os.Args) > 1 {
		query = os.Args[1]
	}

	client := promclient.New(cfg.PrometheusURL, cfg.PrometheusTimeout)
	result, err := client.InstantQuery(context.Background(), query)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prometheus smoke query failed: %v\n", err)
		os.Exit(1)
	}

	payload := map[string]any{
		"ok":     true,
		"url":    cfg.PrometheusURL,
		"query":  query,
		"result": json.RawMessage(result),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode output: %v\n", err)
		os.Exit(1)
	}
}
