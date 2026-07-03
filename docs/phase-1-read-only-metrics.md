# Phase 1 Read-Only Metrics

Goal: prove the Prometheus to Go to React chart flow.

## Implemented

- `GET /api/v1/metrics/query`
- `GET /api/v1/metrics/query-range`
- `GET /api/v1/metrics/labels`
- `GET /api/v1/metrics/label-values`
- `GET /api/v1/metrics/series`
- Request validation for query length, required params, range duration, step size, sample density, and timeout caps.
- React overview dashboard with hardcoded Kubernetes panels.
- uPlot time-series chart component.
- Frontend API client with loading, error, and empty states.
- PromQL instant query workbench.

## Hardcoded Panels

- Cluster CPU usage:
  ```promql
  100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
  ```
- Cluster memory usage:
  ```promql
  (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100
  ```
- Pod restarts:
  ```promql
  sum by (namespace) (increase(kube_pod_container_status_restarts_total[5m]))
  ```

## Verified

- Backend tests passed with `go test ./...`.
- Frontend build passed with `npm run build`.
- Backend `query`, `query-range`, `labels`, and `label-values` endpoints returned real Prometheus data.
- Frontend dev server served on `http://localhost:5173`.
- Frontend proxy returned live backend metrics data through `/api/v1/metrics/query`.

## Notes

- The in-app browser was unavailable in this Codex session, so visual screenshot verification could not be completed from here.
- The frontend still calls only the Go backend; it does not call Prometheus directly.
