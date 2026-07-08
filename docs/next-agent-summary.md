# Next Agent Summary

## Project

- Repo: `E:\Internship\Monitoring tool`
- Remote: `https://github.com/fahadsaleem1678/Monitoring-Tool-.git`
- Branch: `main`
- Product: self-hosted Kubernetes monitoring tool.
- Architecture: React frontend -> Go backend -> Prometheus for metrics; PostgreSQL/Supabase for app data.

## Current Runtime

- Frontend usually runs at `http://localhost:5174`.
- Backend usually runs at `http://localhost:18080`.
- Prometheus is expected at `http://localhost:9090` through Windows port-forwarding.
- Backend uses Supabase/PostgreSQL through `DATABASE_URL`.
- Slack notifications use server-side `SLACK_WEBHOOK_URL`.
- Do not commit or print real Supabase credentials or Slack webhook URLs.

## Completed And Pushed

- Phase 0/1/2 are complete.
- Phase 3 live dashboard update foundation is complete and pushed in commit `b7aa929`.
  - `GET /ws/live`
  - JWT auth through WebSocket `token` query param
  - panel subscriptions
  - query deduplication by PromQL + refresh interval
  - backend fan-out from Prometheus to clients
  - frontend reconnect and polling fallback
- Phase 4 Slack alert notification foundation is complete and pushed in commit `b7aa929`.
  - Slack sender in `backend/internal/notify`
  - alert rule CRUD APIs
  - alert event API
  - Slack test notification API
  - frontend Alerts tab
- Phase 4 automatic alert evaluation is implemented locally after commit `233d8bb`.
  - `backend/internal/alerting` evaluates saved rules in the background.
  - Backend loads rules from PostgreSQL/Supabase every `ALERT_EVAL_INTERVAL_SECONDS` seconds, default `15`.
  - Firing alerts create one open `alert_events` row with status `firing`.
  - Resolved alerts update the open row to status `resolved` with `resolved_at`.
  - Slack notifications are sent once when firing and once when resolved.

## Latest Visualization Work

The user asked for Grafana-like panel visualizations and a title change. The current implementation includes:

- Top heading changed from `Monitoring Overview` to `Monitoring Tool`.
- Custom dashboard panels now support:
  - `line`
  - `bar`
  - `gauge`
- New frontend files:
  - `frontend/src/components/charts/BarChart.tsx`
  - `frontend/src/components/charts/GaugeChart.tsx`
- Modified frontend files:
  - `frontend/src/App.tsx`
  - `frontend/src/api/dashboards.ts`
  - `frontend/src/components/dashboards/DashboardManager.tsx`
  - `frontend/src/components/overview/Panel.tsx`
  - `frontend/src/styles.css`
- The Add Panel form now has visualization, unit, gauge max, and refresh seconds controls.
- Frontend build passed after these changes:

```powershell
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

## Latest Query Builder Work

The user asked for a Visual Query Builder so users can build PromQL without typing it manually. The current local implementation includes:

- New component: `frontend/src/components/query/VisualQueryBuilder.tsx`.
- New frontend API helpers in `frontend/src/api/metrics.ts`:
  - `listMetricNames()`
  - `listLabelValues(label)`
- Query builder is embedded in:
  - `frontend/src/components/overview/QueryWorkbench.tsx`
  - `frontend/src/components/dashboards/DashboardManager.tsx`
- Users can select/search a metric, add label filters, choose aggregation, optionally group by labels, optionally wrap counters in `rate(metric[window])`, preview the generated PromQL, and apply it to the current query field.
- Frontend build passed after these changes:

```powershell
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

## Latest Dashboard Editing And Operations Work

The current dashboard UI also includes:

- Click-to-edit saved panels in `frontend/src/components/dashboards/DashboardManager.tsx`.
- Frontend `updatePanel()` API helper in `frontend/src/api/dashboards.ts`.
- Template variables at the top of each dashboard.
  - Variables like `$namespace` are substituted into panel PromQL at runtime.
  - Variable values are loaded from Prometheus label values.
  - Variable definitions are stored in browser `localStorage`.
- Dashboard-level time controls:
  - Last 15m, 1h, 6h, 24h.
  - Auto refresh off, 15s, 30s, 1m, 5m.
  - Manual refresh and last refresh timestamp.
- AI incident summaries for alert firing/resolved messages.
  - Backend files: `backend/internal/alerting/incident.go` and tests in `incident_test.go`.
  - Slack and alert event messages now include impact, likely cause, observed threshold condition, and suggested checks.
  - Specific command hints exist for kube-state-metrics, node-exporter, Prometheus, restarts, CPU, memory, and deployment health.
  - Alert event messages preserve multiline formatting in the UI.

## Important Known Issues / Next Work

- Current Alerts tab can create rules, send Slack test notifications, and list alert events. Event refresh is manual from the UI.
- Automatic alert pending state is in memory. If the backend restarts, `for_seconds` pending timers restart, but existing open firing events are still read from PostgreSQL.
- Prometheus port-forward must be active for charts and backend readiness.
- If charts show `No data`, check `http://localhost:9090/api/v1/query?query=up`.
- K3s deployment phase is next. See `docs/k3s-deployment-handoff.md`.

## Useful Verification Commands

```powershell
docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

Clean frontend build artifacts after build:

```powershell
$targets = @('frontend\dist','frontend\tsconfig.node.tsbuildinfo','frontend\tsconfig.tsbuildinfo','frontend\vite.config.d.ts','frontend\vite.config.js')
$root = (Resolve-Path '.').Path
foreach ($target in $targets) {
  if (Test-Path -LiteralPath $target) {
    $resolved = (Resolve-Path -LiteralPath $target).Path
    if ($resolved -like "$root*") {
      Remove-Item -LiteralPath $resolved -Recurse -Force
    } else {
      throw "Refusing to remove outside workspace: $resolved"
    }
  }
}
```

## Security Notes

- `SLACK_WEBHOOK_URL` is a secret. Keep it server-side only.
- `DATABASE_URL` is a secret. Keep it as an environment variable only.
- Supabase service-role keys must never be used in frontend code.
- Earlier credentials/webhook values were pasted in chat during testing; recommend rotating them before real deployment or public demo.
