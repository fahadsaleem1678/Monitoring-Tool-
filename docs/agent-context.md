# Agent Handoff Context

## Project

- Repo root: `E:\Internship\Monitoring tool`
- GitHub remote: `https://github.com/fahadsaleem1678/Monitoring-Tool-.git`
- Branch: `main`
- Product: self-hosted Kubernetes monitoring tool, Grafana/Datadog-inspired.
- Architecture: React frontend -> Go backend -> Prometheus for metrics; PostgreSQL/Supabase for app data only.

## User Workflow Rules

- Work phase by phase.
- Do not start the next phase until the user explicitly asks.
- Ask only when genuinely blocked.
- Never print, log, commit, or echo database credentials or Supabase keys.
- Supabase service-role keys must never be put in frontend code.
- Use `DATABASE_URL` only as an environment variable, never committed to files.

## Environment

- User has a K3s cluster running on an AlmaLinux VM.
- Prometheus is reachable from Windows through port-forwarding at `http://localhost:9090`.
- Confirmed Prometheus namespace/service from Phase 0:
  - namespace: `monitoring`
  - service: `prometheus-stack-kube-prom-prometheus`
  - in-cluster URL candidate: `http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090`
- Local Docker is used for Go/Node verification because Go/Node are not installed on the Windows PATH in this session.

## Completed Work

### Phase 0

- Project skeleton created.
- Backend health/readiness endpoints added.
- Prometheus smoke query command added.
- Frontend Vite skeleton added.
- Docker Compose local Postgres added.
- Discovery docs added.

### Phase 1

- Prometheus backend endpoints added:
  - `GET /api/v1/metrics/query`
  - `GET /api/v1/metrics/query-range`
  - `GET /api/v1/metrics/labels`
  - `GET /api/v1/metrics/label-values`
  - `GET /api/v1/metrics/series`
- Query validation added for range, step, timeout, and query length.
- Frontend monitoring overview added.
- uPlot charts added.
- PromQL query workbench added.
- Verified with live Prometheus data.
- Pushed as commit `05c6e11`.

### Phase 2

- Auth and persistence implemented locally:
  - bcrypt password hashes
  - hand-rolled HMAC JWT service
  - `admin` and `viewer` seeded from env vars
  - admin/viewer role handling
  - dashboard CRUD
  - panel CRUD
  - panel preview endpoint
- Frontend login and dashboard manager added.
- Saved dashboards/panels render Prometheus data through backend.
- Migration files added under `backend/migrations/`.
- `backend/cmd/migrate` added for migration-only runs.
- `scripts/migrate-supabase.ps1` added. It prompts for the Supabase Postgres URI using hidden input and passes it to Docker as `DATABASE_URL`.

### Phase 3

- Live dashboard updates started:
  - `GET /ws/live` WebSocket endpoint added.
  - WebSocket auth validates the existing JWT through a `token` query parameter.
  - Backend live manager accepts panel subscription messages.
  - Backend deduplicates identical PromQL + refresh interval subscriptions across connected clients.
  - Backend uses one ticker per unique query and fans out Prometheus instant-query results to subscribers.
  - Query execution uses timeout/cancellation through context.
  - Frontend WebSocket client added.
  - Saved dashboard panels append live vector samples to existing chart series.
  - Frontend reconnects after disconnects and uses range-query polling fallback while disconnected.
  - Vite dev proxy now forwards `/ws` WebSocket traffic to the backend.

### Phase 4

- Slack alert notification foundation started:
  - Server-side Slack incoming webhook sender added under `backend/internal/notify`.
  - Slack webhook URL is read from `SLACK_WEBHOOK_URL`; it is never committed and should not be exposed to frontend code.
  - Alert rule APIs added:
    - `GET /api/v1/alerts/rules`
    - `POST /api/v1/alerts/rules`
    - `PUT /api/v1/alerts/rules/{id}`
    - `DELETE /api/v1/alerts/rules/{id}`
  - Alert event API added:
    - `GET /api/v1/alerts/events`
  - Slack test notification API added:
    - `POST /api/v1/alerts/test-notification`
  - Frontend Alerts tab added with rule creation, rule listing/deletion, recent event history, and Slack test notification action.
  - Automatic alert evaluation loop is not implemented yet; next Phase 4 step is evaluating enabled rules on a scheduler and creating firing/resolved events.

## Verification So Far

Commands that passed:

```powershell
docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

- `http://localhost:9090/api/v1/query?query=up` returned success from Prometheus on July 6, 2026.
- Supabase pooler host `aws-1-ap-southeast-2.pooler.supabase.com:5432` was reachable from Windows on July 6, 2026.
- Backend migration against Supabase completed successfully on July 6, 2026 using the pooler URI with URL-encoded password and `sslmode=require`.
- Backend runtime readiness against Supabase completed successfully on July 6, 2026:
  - temporary backend container ran on `http://localhost:18080`
  - `PROMETHEUS_URL=http://host.docker.internal:9090`
  - `GET /readyz` returned `status: ready` and `prometheus: ready`
  - temporary test container was stopped afterward.
- Overview chart panels initially showed `No data` because frontend range queries used the Windows/browser clock while Prometheus data was several hours behind that clock.
- `frontend/src/api/metrics.ts` was updated to anchor range queries to Prometheus `time()` with a short cache; CPU, memory, and restart range queries then returned non-empty matrix data.
- Phase 4 alert API smoke test passed on July 6, 2026:
  - backend `/readyz` returned ready
  - admin login succeeded
  - `GET /api/v1/alerts/rules` returned successfully
  - `GET /api/v1/alerts/events` returned successfully
  - temporary alert rule create/delete through the backend returned successfully
- Frontend build passed after adding Alerts view:
  - `docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build`
- Backend was restarted with `SLACK_WEBHOOK_URL` on July 6, 2026.
- Slack test notification through `POST /api/v1/alerts/test-notification` succeeded and stored an alert event with status `notification_sent`.

Runtime checks passed locally:

- Admin login works.
- Viewer login works.
- Viewer can list dashboards.
- Viewer cannot create dashboards (`403`).
- Dashboard create works.
- Panel create works.
- Saved dashboard fetch returns persisted panels.
- Panel preview returns live Prometheus vector data.

## Current Runtime Ports Used During Testing

- Phase 1 backend: `http://localhost:8080`
- Phase 1 frontend: `http://localhost:5173`
- Phase 2 backend: `http://localhost:18080`
- Phase 2 frontend: `http://localhost:5174`
- Local Postgres: `localhost:5432`

These containers may or may not still be running in a future session. Check with:

```powershell
docker ps --filter name=monitoring-tool
```

## Supabase Migration Status

- User created/selected a Supabase project.
- Screenshot showed `public` schema had no tables before migration.
- A Postgres URI was provided in chat, but do not echo it or place it in commands/files.
- Recommended safe migration path:

```powershell
cd "E:\Internship\Monitoring tool"
.\scripts\migrate-supabase.ps1
```

The script prompts for the DB URI with hidden input, appends `sslmode=require` if needed, runs:

```text
go run ./cmd/migrate
```

inside Docker, then clears the environment variable.

## Important Security Notes

- Do not use or expose Supabase service-role key in frontend.
- Do not commit `.env` or any real connection string.
- If a service-role key was pasted into chat, recommend rotating it before real deployment.
- Tables in `public` have RLS enabled in migration SQL and runtime schema setup.
- Current backend connects server-side with `DATABASE_URL`; frontend does not use Supabase APIs directly.

## Next Expected Step

Likely next steps are:

- User runs Supabase migration helper locally.
- Verify Supabase tables appear.
- Start backend against Supabase `DATABASE_URL` using environment variables only.
- Continue Phase 4 with the automatic alert evaluator loop:
  - periodically evaluate enabled PromQL alert rules
  - create firing/resolved alert events
  - send Slack notifications only on state changes
  - keep `SLACK_WEBHOOK_URL` server-side only.
