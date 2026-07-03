# Monitoring Tool

Self-hosted Kubernetes monitoring product inspired by Grafana and Datadog.
Phase 0 establishes the project foundation and verifies connectivity.

## Current Phase

Phase 0 only:

- Go backend skeleton.
- Health and readiness endpoints.
- Prometheus smoke query endpoint.
- React + Vite frontend skeleton.
- Docker Compose PostgreSQL for local development.
- Migration folder placeholder.

## Prerequisites

- Go 1.22 or newer.
- Node.js 20 or newer.
- Docker Desktop or Docker Engine with Compose.
- A reachable Prometheus URL from Windows/local development.

## Configuration

Copy the example file and update `PROMETHEUS_URL`:

```powershell
Copy-Item .env.example .env
```

For local development with your current port-forward, `PROMETHEUS_URL` will
usually look like:

```text
http://localhost:9090
```

When Supabase credentials are available, replace `DATABASE_URL` with the
Supabase PostgreSQL connection string. Do not put service-role API keys in the
frontend.

## Start PostgreSQL

```powershell
docker compose up -d postgres
docker compose ps
```

## Start Backend

```powershell
cd backend
go run ./cmd/server
```

Health checks:

```powershell
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/api/v1/metrics/prometheus-smoke
```

Prometheus smoke CLI:

```powershell
go run ./cmd/prom-smoke up
```

## Start Frontend

```powershell
cd frontend
npm install
npm run dev
```

Open:

```text
http://localhost:5173
```

The page should show backend health through the Vite proxy.

## Phase 0 Success Criteria

- PostgreSQL container is healthy.
- Backend starts locally.
- `/healthz` returns `healthy`.
- `/readyz` can reach Prometheus.
- `/api/v1/metrics/prometheus-smoke` returns sample Prometheus JSON.
- Frontend starts locally and displays backend health.

## Project Structure

```text
backend/
  cmd/server/
  internal/config/
  internal/health/
  internal/prometheus/
  migrations/
frontend/
  src/
deploy/
docs/
```
