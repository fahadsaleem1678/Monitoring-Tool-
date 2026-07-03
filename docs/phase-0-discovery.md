# Phase 0 Discovery

Use this document to capture the environment facts before Phase 1 starts.

## Prometheus

- Windows/local URL: `http://localhost:9090`
- In-cluster URL: `http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090`
- Namespace: `monitoring`
- Service name: `prometheus-stack-kube-prom-prometheus`
- Port: `9090`

## Expected Kubernetes Metrics

Confirmed from the Windows/local Prometheus API:

```promql
up                                  # success, includes Prometheus, kubelet, Grafana, Alertmanager, kube-state-metrics, node-exporter
node_cpu_seconds_total              # success, node-exporter metrics present
kube_node_status_condition          # success, kube-state-metrics present
container_cpu_usage_seconds_total   # success, cAdvisor/kubelet metrics present
```

## Phase 0 Test Log

- Backend compile: passed with `docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...`.
- Backend `/healthz`: passed from temporary Go container on `localhost:8080`.
- Backend `/readyz`: passed from temporary Go container with `PROMETHEUS_URL=http://host.docker.internal:9090`.
- Prometheus smoke endpoint: passed from backend container with real `up` JSON from the K3s Prometheus.
- Frontend build: passed with `docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build`.
- Frontend health card path: Vite dev server served on `localhost:5173`, and `/api/v1/health` proxied to the backend successfully.
- PostgreSQL Compose health: healthy, container `monitoring-tool-postgres`.

## Notes

- Prometheus remains the metrics source of truth.
- PostgreSQL/Supabase stores application data only.
- The frontend calls the Go backend only.
