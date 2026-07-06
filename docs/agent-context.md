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

## Verification So Far

Commands that passed:

```powershell
docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

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

The user asked to create this handoff context and push all current work. After that, likely next steps are:

- User runs Supabase migration helper locally.
- Verify Supabase tables appear.
- Start backend against Supabase `DATABASE_URL` using environment variables only.
- Then continue Phase 2 testing or move to Phase 3 only if user explicitly asks.
