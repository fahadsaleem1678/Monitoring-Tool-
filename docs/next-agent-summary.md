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

## Important Known Issues / Next Work

- Phase 4 automatic alert evaluation is not implemented yet.
- Next backend work:
  - periodically load enabled alert rules
  - run each PromQL instant query
  - compare result values against operator/threshold
  - honor `for_seconds`
  - create firing/resolved `alert_events`
  - send Slack notifications only on state transitions
- Current Alerts tab can create rules and send Slack test notifications, but saved alert rules do not auto-fire yet.
- Prometheus port-forward must be active for charts and backend readiness.
- If charts show `No data`, check `http://localhost:9090/api/v1/query?query=up`.

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
