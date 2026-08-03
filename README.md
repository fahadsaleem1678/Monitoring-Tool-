# Monitoring Tool

Self-hosted Kubernetes monitoring product inspired by Grafana and Datadog.
Phase 0 establishes the project foundation and verifies connectivity.

## Current Phase

Phase 2:

- Go backend skeleton.
- Health and readiness endpoints.
- Prometheus smoke query endpoint.
- Validated Prometheus instant query endpoint.
- Validated Prometheus range query endpoint.
- Metrics labels, label values, and series endpoints.
- React + Vite frontend skeleton.
- Kubernetes overview with hardcoded Phase 1 panels.
- PromQL instant query workbench.
- Username/password login with JWT.
- Admin seed user.
- Saved dashboard and panel APIs.
- Dashboard list/detail UI with saved PromQL panels.
- Docker Compose PostgreSQL for local development.
- Migration files for app schema.
- AI incident reviews with read-only MCP evidence and local Ollama draft generation.

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

Default local login:

```text
admin / admin123
viewer / viewer123
```

## Incident Agent LLM Mode

The incident agent defaults to local Ollama:

```text
LLM_PROVIDER=ollama
OLLAMA_URL=http://host.docker.internal:11434
OLLAMA_MODEL=qwen2.5:7b
```

For a local Ollama demo, start Ollama on the host and pull the model:

```powershell
ollama pull qwen2.5:7b
ollama serve
```

The agent still owns the investigation sequence. It collects Prometheus and Kubernetes evidence through read-only MCP endpoints, then asks the LLM to produce a summary, probable cause, confidence, suggested checks, and a Slack draft. Slack broadcast still requires an admin to approve the draft first.

Safety limits:

```text
MAX_PROMETHEUS_QUERY_RANGE_MINUTES=60
MAX_LOG_LINES=80
```

OpenAI can be used instead by setting:

```text
LLM_PROVIDER=openai
OPENAI_API_KEY=...
OPENAI_MODEL=gpt-4.1-mini
```

## Official Prometheus MCP

The Kubernetes manifests include the current custom Prometheus MCP service and a parallel official MCP service:

```text
prometheus-mcp:8091            current MVP HTTP shim
prometheus-mcp-official:8080   official ghcr.io/pab1it0/prometheus-mcp-server
```

The agent uses the MVP shim by default:

```text
PROMETHEUS_MCP_MODE=mvp
```

After the official pod is healthy, switch the agent to it:

```bash
kubectl -n monitoring-tool patch configmap monitoring-tool-config \
  --type merge \
  -p '{"data":{"PROMETHEUS_MCP_MODE":"official"}}'

kubectl rollout restart deployment/incident-agent -n monitoring-tool
```

The official server is configured for HTTP stateless MCP and exposes read-only tools such as instant query, range query, targets, metric metadata, and metric listing.

Debug the active Prometheus MCP path through the incident agent:

```bash
kubectl -n monitoring-tool port-forward svc/incident-agent 8090:8090
curl http://localhost:8090/debug/prometheus-mcp
curl http://localhost:8090/debug/prometheus-mcp-official
```

`/debug/prometheus-mcp` checks the currently configured mode. `/debug/prometheus-mcp-official` forces the official MCP path and reports whether it can list tools and run a simple `up` query.

## Official Kubernetes MCP

The Kubernetes manifests also include the current custom Kubernetes MCP service and a parallel official MCP service:

```text
kubernetes-mcp:8091            current MVP HTTP shim
kubernetes-mcp-official:8080   official containers/kubernetes-mcp-server
```

The agent uses the MVP shim by default:

```text
KUBERNETES_MCP_MODE=mvp
```

After the official pod is healthy, smoke test it through the incident agent:

```bash
kubectl -n monitoring-tool port-forward svc/incident-agent 8090:8090
curl http://localhost:8090/debug/kubernetes-mcp
curl http://localhost:8090/debug/kubernetes-mcp-official
```

Switch the agent to official Kubernetes MCP:

```bash
kubectl -n monitoring-tool patch configmap monitoring-tool-config \
  --type merge \
  -p '{"data":{"KUBERNETES_MCP_MODE":"official"}}'

kubectl rollout restart deployment/incident-agent -n monitoring-tool
```

Official Kubernetes MCP is deployed read-only, stateless, core-only, and with multi-cluster disabled. The agent keeps MVP fallback enabled.

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

Run database migrations only:

```powershell
cd "E:\Internship\Monitoring tool"
.\scripts\migrate-supabase.ps1
```

Phase 1 metrics endpoints:

```powershell
curl "http://localhost:8080/api/v1/metrics/query?query=sum(up)"

$end = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$start = $end - 600
curl "http://localhost:8080/api/v1/metrics/query-range?query=sum(up)&start=$start&end=$end&step=60"

curl "http://localhost:8080/api/v1/metrics/labels"
curl "http://localhost:8080/api/v1/metrics/label-values?label=namespace"
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

## Phase 1 Success Criteria

- Backend instant and range query endpoints return real Prometheus data.
- Query validation rejects missing queries and overly dense range requests.
- Frontend overview loads real cluster summary data through the Go backend.
- Charts render range data from Prometheus through backend polling.
- Query failures are shown in the UI.

## Phase 2 Success Criteria

- Admin can log in.
- Dashboard and panel data persists in PostgreSQL.
- Admin can create and delete dashboards.
- Admin can add and delete PromQL panels.
- Saved panels render real Prometheus data through the backend.
- Frontend never calls Prometheus or Supabase directly.

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
