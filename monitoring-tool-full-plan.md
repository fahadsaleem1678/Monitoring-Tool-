# Self-Hosted Kubernetes Monitoring Tool - Full Project Plan

## 1. Executive Summary

The project is to build a self-hosted observability platform similar in spirit to Grafana, Datadog, and New Relic, focused first on Kubernetes metrics. Prometheus is already scraping the K3s cluster running on the AlmaLinux VM, so the new platform should not collect or store raw time-series metrics itself in v1. Instead, Prometheus remains the metrics source of truth, while the new Go backend provides authentication, dashboard management, PromQL query APIs, alert orchestration, and live update fan-out. The React frontend provides the operator experience: dashboards, panels, query editing, alerts, and cluster health views.

Recommended v1 scope: metrics-only monitoring for the existing Kubernetes cluster, with dashboards, saved panels, PromQL queries, user login, and basic alerting. Logs, traces, multi-cluster support, long-term storage, and complex anomaly detection should be later phases.

## 2. Product Goals

Primary goals:

- Show real-time and historical Kubernetes metrics from Prometheus.
- Provide a polished dashboard UI with panels like CPU, memory, pod health, node health, network, and restarts.
- Allow users to create dashboards and panels using PromQL.
- Support authentication and simple roles.
- Support alerts for threshold conditions and service health failures.
- Deploy inside the same K3s cluster on the AlmaLinux VM.

Non-goals for v1:

- Replacing Prometheus TSDB.
- Building a full Grafana plugin system.
- Building a complete Datadog clone.
- Logs, tracing, APM, RUM, synthetic monitoring, and incident management.
- Multi-tenant billing or SaaS features.

## 3. Recommended Architecture

Use a pragmatic Grafana-style architecture:

- Kubernetes and kube-state-metrics expose metrics.
- Prometheus scrapes and stores metrics.
- Go backend queries Prometheus through its HTTP API.
- PostgreSQL stores application data only: users, dashboards, panels, alert rules, notification settings, and alert history.
- React frontend talks only to the Go backend, never directly to Prometheus.
- WebSocket support is added after the read-only MVP to provide live dashboard updates efficiently.

This keeps the system achievable and avoids rebuilding Prometheus or Grafana internally.

## 4. Major Components

### 4.1 Go Backend

Responsibilities:

- Load configuration from environment variables.
- Connect to Prometheus.
- Connect to PostgreSQL.
- Expose REST APIs.
- Validate JWT authentication.
- Proxy PromQL instant and range queries.
- Store and retrieve dashboards and panels.
- Evaluate or integrate alert rules.
- Push live metric updates over WebSocket.
- Expose backend health and readiness endpoints.

Recommended Go stack:

- Go 1.22 or newer.
- HTTP router: chi.
- Prometheus client: official `github.com/prometheus/client_golang/api/prometheus/v1`.
- Database driver: pgx.
- Query layer: sqlc, or pgx with a small repository layer.
- Migrations: golang-migrate.
- Auth: bcrypt password hashes and signed JWT tokens.
- WebSocket: nhooyr.io/websocket or gorilla/websocket.
- Config: plain environment variables for v1.

Suggested backend structure:

```text
backend/
  cmd/server/main.go
  internal/config/
  internal/httpapi/
  internal/auth/
  internal/prometheus/
  internal/store/
  internal/dashboards/
  internal/alerts/
  internal/live/
  internal/health/
  migrations/
  Dockerfile
```

### 4.2 React Frontend

Responsibilities:

- Login/logout flow.
- Main application shell with navigation.
- Dashboard list.
- Dashboard view.
- Dashboard editor.
- Panel editor.
- PromQL query editor.
- Chart rendering.
- Alert rules and alert history.
- Basic cluster overview page.

Recommended frontend stack:

- React + Vite + TypeScript.
- TanStack Query for API state.
- React Router for navigation.
- uPlot for high-performance time-series charts.
- react-grid-layout for dashboard panel placement.
- Zustand or lightweight context for UI state.
- Monaco editor or CodeMirror later for a better PromQL editor.

Initial pages:

- Login.
- Overview.
- Dashboards.
- Dashboard detail.
- Panel editor.
- Alerts.
- Settings.

### 4.3 PostgreSQL App Database

The database stores app configuration and user-created resources, not raw metrics.

Core tables:

```text
users
sessions or jwt_key_versions
dashboards
panels
alert_rules
alert_events
notification_channels
audit_events
```

Minimum data model:

```sql
users (
  id uuid primary key,
  username text unique not null,
  password_hash text not null,
  role text not null,
  created_at timestamptz not null
)

dashboards (
  id uuid primary key,
  title text not null,
  description text,
  owner_id uuid references users(id),
  created_at timestamptz not null,
  updated_at timestamptz not null
)

panels (
  id uuid primary key,
  dashboard_id uuid references dashboards(id) on delete cascade,
  title text not null,
  promql text not null,
  visualization_type text not null,
  grid_x int not null,
  grid_y int not null,
  grid_w int not null,
  grid_h int not null,
  refresh_interval_seconds int not null,
  settings_json jsonb not null default '{}'
)

alert_rules (
  id uuid primary key,
  name text not null,
  promql text not null,
  operator text not null,
  threshold double precision not null,
  for_seconds int not null,
  severity text not null,
  enabled boolean not null,
  created_at timestamptz not null,
  updated_at timestamptz not null
)

alert_events (
  id uuid primary key,
  rule_id uuid references alert_rules(id),
  status text not null,
  value double precision,
  message text,
  started_at timestamptz not null,
  resolved_at timestamptz
)
```

## 5. API Plan

### 5.1 Auth

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/me
```

### 5.2 Metrics

```text
GET /api/v1/metrics/query
GET /api/v1/metrics/query-range
GET /api/v1/metrics/labels
GET /api/v1/metrics/label-values
GET /api/v1/metrics/series
```

### 5.3 Dashboards

```text
GET    /api/v1/dashboards
POST   /api/v1/dashboards
GET    /api/v1/dashboards/{id}
PUT    /api/v1/dashboards/{id}
DELETE /api/v1/dashboards/{id}
```

### 5.4 Panels

```text
POST   /api/v1/dashboards/{id}/panels
PUT    /api/v1/panels/{id}
DELETE /api/v1/panels/{id}
POST   /api/v1/panels/preview
```

### 5.5 Alerts

```text
GET    /api/v1/alerts/rules
POST   /api/v1/alerts/rules
PUT    /api/v1/alerts/rules/{id}
DELETE /api/v1/alerts/rules/{id}
GET    /api/v1/alerts/events
POST   /api/v1/alerts/test-notification
```

### 5.6 Live Updates

```text
GET /ws/live
```

WebSocket message types:

```json
{"type":"subscribe","panel_ids":["panel-1","panel-2"]}
{"type":"unsubscribe","panel_ids":["panel-1"]}
{"type":"metric_update","panel_id":"panel-1","data":{}}
{"type":"error","message":"query failed"}
```

## 6. Phased Roadmap

### Phase 0 - Discovery and Foundation

Goal: confirm environment, connectivity, and project skeleton.

Tasks:

- Confirm Prometheus URL from inside the cluster.
- Confirm Prometheus URL from local development machine.
- Identify the namespace where Prometheus is running.
- Confirm available metrics: node exporter, kube-state-metrics, cAdvisor, kubelet metrics.
- Create repository structure with backend, frontend, deploy, and docs folders.
- Create local dev setup with Docker Compose for PostgreSQL.
- Add backend config loading.
- Add backend health endpoint.
- Add database migration tooling.
- Add frontend Vite app skeleton.

Deliverables:

- Backend starts locally.
- Frontend starts locally.
- PostgreSQL starts locally.
- Prometheus connectivity test works.
- Initial README with setup steps.

Success criteria:

- A Go command can query Prometheus and return sample JSON.
- `/healthz` returns healthy.
- Frontend can call backend health endpoint.

Estimated duration: 3 to 5 working days.

### Phase 1 - Read-Only Metrics MVP

Goal: prove Prometheus to Go to React chart flow.

Tasks:

- Implement Prometheus client wrapper.
- Implement instant query endpoint.
- Implement range query endpoint.
- Add safe request validation for query, start, end, step, and timeout.
- Build React overview page.
- Add initial hardcoded panels:
  - Cluster CPU usage.
  - Cluster memory usage.
  - Node status.
  - Pod count by namespace.
  - Pod restarts.
- Add chart component with uPlot.
- Add frontend API client.
- Add loading, empty, and error states.

Deliverables:

- Working read-only monitoring UI.
- Hardcoded Kubernetes dashboard.
- Basic PromQL query page for manual testing.

Success criteria:

- User can open the UI and see real metrics from the K3s cluster.
- Charts update through polling.
- Query failures are shown cleanly in the UI.

Estimated duration: 1 to 2 weeks.

### Phase 2 - Auth, Users, Dashboards, and Panels

Goal: turn the MVP into a usable dashboard product.

Tasks:

- Add users table and migrations.
- Add admin seed user or setup command.
- Implement login with bcrypt and JWT.
- Add auth middleware.
- Add role model: admin and viewer.
- Implement dashboard CRUD APIs.
- Implement panel CRUD APIs.
- Build login page.
- Build dashboard list page.
- Build dashboard view page.
- Build dashboard editor with add, edit, delete, resize, and reorder panels.
- Store panel layout in PostgreSQL.
- Add panel preview before saving.
- Add metric labels and label values endpoints.
- Add PromQL autocomplete basics.

Deliverables:

- Users can log in.
- Users can create dashboards.
- Users can add PromQL panels.
- Dashboards persist after refresh.

Success criteria:

- Admin can create and edit dashboards.
- Viewer can view dashboards but cannot edit.
- Panel layout persists correctly.
- Frontend never calls Prometheus directly.

Estimated duration: 2 to 3 weeks.

### Phase 3 - Live Dashboard Updates

Goal: make dashboards feel real-time without every browser tab hammering Prometheus.

Tasks:

- Add WebSocket endpoint.
- Implement client connection manager.
- Implement panel subscription messages.
- Deduplicate identical PromQL queries across active users.
- Add backend ticker per unique query and refresh interval.
- Push metric updates to subscribed clients.
- Add frontend WebSocket client.
- Add reconnect behavior.
- Add polling fallback if WebSocket disconnects.
- Add query timeout and cancellation.

Deliverables:

- Live dashboard refresh over WebSocket.
- Efficient backend fan-out from Prometheus to many users.

Success criteria:

- Multiple browser tabs viewing the same panel do not create duplicate Prometheus load.
- UI shows live updates smoothly.
- Disconnects recover automatically.

Estimated duration: 1 to 2 weeks.

### Phase 4 - Alerting

Goal: provide practical alert visibility and notifications.

Recommended decision: integrate with Prometheus Alertmanager first if already available or easy to install. If not, implement a simple internal alert evaluator for v1.

Option A: Alertmanager integration:

- Let Prometheus and Alertmanager evaluate alert rules.
- Backend reads active alerts and rule state.
- UI displays active, pending, firing, and resolved alerts.
- Backend stores local alert event history.
- Add notification channel management only if needed.

Option B: Internal evaluator:

- Store alert rules in PostgreSQL.
- Scheduler evaluates PromQL queries.
- State machine: OK, Pending, Firing, Resolved.
- Notifications via webhook and email.
- Store alert event history.

Recommended for this project: Option A if the manager wants production-like behavior, Option B if the assignment specifically requires implementing alert logic in Go.

Tasks:

- Confirm alerting approach.
- Build alert rules page.
- Build alert history page.
- Add notification channels.
- Add test notification action.
- Add backend audit logs for alert changes.
- Add runbook URL field for alert rules.

Deliverables:

- Alert rule management or Alertmanager visibility.
- Alert history.
- Webhook or email notification.

Success criteria:

- A test rule can fire and resolve.
- Users can see active and historical alerts.
- Notification delivery is confirmed.

Estimated duration: 2 to 3 weeks.

### Phase 5 - Kubernetes Deployment

Goal: deploy the product into the AlmaLinux K3s cluster.

Tasks:

- Create backend Dockerfile.
- Create frontend Dockerfile with Nginx or static server.
- Create Kubernetes manifests:
  - Namespace.
  - PostgreSQL StatefulSet or external database config.
  - Backend Deployment.
  - Frontend Deployment.
  - Services.
  - ConfigMaps.
  - Secrets.
  - Ingress.
- Use internal DNS for Prometheus URL.
- Add resource requests and limits.
- Add liveness and readiness probes.
- Add database migration job.
- Add persistent volume for PostgreSQL if using in-cluster DB.
- Configure Traefik ingress if using default K3s ingress.

Deliverables:

- Product accessible through cluster ingress.
- Backend can query in-cluster Prometheus.
- Database persists across pod restarts.

Success criteria:

- Fresh deployment works from manifests.
- Restarting backend/frontend does not lose dashboards.
- Prometheus remains internal and protected.

Estimated duration: 1 week.

### Phase 6 - Polish, Security, and Manager Demo

Goal: make the project demo-ready and maintainable.

Tasks:

- Add default Kubernetes dashboard templates.
- Add dashboard import/export JSON.
- Add better PromQL editor experience.
- Add audit logs for dashboard and alert changes.
- Improve visual design: dark theme, dense dashboard layout, readable charts.
- Add backend structured logging.
- Add frontend error boundary.
- Add API rate limits for expensive PromQL queries.
- Add documentation:
  - Local setup.
  - K3s deployment.
  - Architecture.
  - API overview.
  - Demo script.

Deliverables:

- Demo-ready application.
- Documentation.
- Known limitations list.

Success criteria:

- Manager can see a polished dashboard, create a panel, edit PromQL, and trigger a test alert.
- A new developer can run the project from README steps.

Estimated duration: 1 to 2 weeks.

## 7. Suggested Milestones

Milestone 1: Connectivity Proof

- Backend queries Prometheus successfully.
- React displays one chart.

Milestone 2: Monitoring MVP

- Overview dashboard displays real cluster metrics.
- Query page supports PromQL.

Milestone 3: Product MVP

- Login works.
- Dashboards and panels are saved.
- Users can build dashboards.

Milestone 4: Live and Alerting

- WebSocket updates work.
- Alerts can fire and resolve.

Milestone 5: Cluster Deployment

- App runs inside K3s.
- Ingress exposes frontend.
- Backend reaches Prometheus internally.

Milestone 6: Demo Release

- UI polish.
- Documentation.
- Demo script.

## 8. Recommended Team Task Breakdown

Backend developer:

- Go project setup.
- Prometheus integration.
- REST APIs.
- Auth.
- PostgreSQL migrations.
- Alerting.
- WebSocket live updates.
- Kubernetes backend manifests.

Frontend developer:

- React app setup.
- App shell and routing.
- Dashboard list and dashboard view.
- Chart components.
- Panel editor.
- Alert pages.
- Login and role-based UI.
- Frontend Dockerfile.

DevOps or shared:

- K3s deployment.
- Secrets and ConfigMaps.
- Ingress.
- PostgreSQL persistence.
- Prometheus service discovery.
- Release documentation.

## 9. Testing Strategy

Backend tests:

- Unit tests for Prometheus query parameter validation.
- Unit tests for auth token creation and validation.
- Unit tests for alert state transitions.
- Repository tests against test PostgreSQL.
- Handler tests for API endpoints.

Frontend tests:

- Component tests for charts, dashboard cards, and forms.
- API client tests with mocked responses.
- Login flow test.
- Dashboard create/edit flow test.

Integration tests:

- Backend against real or local Prometheus.
- Backend against PostgreSQL.
- Frontend to backend smoke test.

Deployment tests:

- `kubectl rollout status`.
- Backend health endpoint.
- Frontend loads through ingress.
- Prometheus query works inside backend pod.

## 10. Key Risks and Mitigations

Risk: Project becomes too large by trying to clone Datadog fully.

Mitigation: Keep v1 metrics-only and Prometheus-backed.

Risk: PromQL queries overload Prometheus.

Mitigation: Add query timeout, max range, max step density, caching later, and WebSocket query deduplication.

Risk: Dashboard editor takes too long.

Mitigation: Start with fixed panels, then add editing and grid layout.

Risk: Alerting scope becomes complex.

Mitigation: Prefer Alertmanager integration unless custom alert engine is explicitly required.

Risk: Single VM has limited resources.

Mitigation: Use resource limits, lightweight charting, and avoid storing metrics twice.

Risk: Authentication becomes overbuilt.

Mitigation: Use simple username/password, bcrypt, JWT, and admin/viewer roles for v1.

## 11. Demo Script

1. Open monitoring UI.
2. Log in as admin.
3. Show cluster overview: CPU, memory, node health, pod count.
4. Open a dashboard with Kubernetes panels.
5. Create a new panel using a PromQL query.
6. Resize or move the panel.
7. Save dashboard and refresh page to prove persistence.
8. Open alert rules.
9. Trigger or simulate an alert.
10. Show alert history and notification result.
11. Explain architecture: Prometheus stores metrics, Go manages product logic, React displays dashboards.

## 12. Final Recommendation

Build this as a metrics-first, self-hosted monitoring platform. The best architecture is not to compete with Prometheus, but to build a product layer on top of it. The Go backend should be the secure API, dashboard, alert, and live-update layer. The React frontend should focus on a professional operator experience. This gives your manager a realistic Datadog/Grafana-like project while keeping the engineering scope achievable.

