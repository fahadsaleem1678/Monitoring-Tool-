# K3s Deployment Handoff

## Purpose

This file gives the next agent enough context to continue from the current local monitoring tool state into the K3s deployment phase.

Do not commit real secrets. Use placeholders in files and inject real values through Kubernetes Secrets or runtime environment variables.

## Current Repo State

- Repo path: `E:\Internship\Monitoring tool`
- Branch: `main`
- Remote: `https://github.com/fahadsaleem1678/Monitoring-Tool-.git`
- Product: self-hosted Kubernetes monitoring tool.
- Stack:
  - React + Vite frontend
  - Go backend
  - Prometheus for metrics
  - PostgreSQL/Supabase for app data
  - Slack webhook for alert notifications

## Recently Added Local Features

These are part of the latest local work that should be pushed before deployment work starts:

- Existing dashboard panels can be edited by clicking the panel.
  - Uses existing backend `PUT /api/v1/panels/{id}`.
  - Frontend API helper: `updatePanel`.
  - Edit form supports title, PromQL, visualization, unit, gauge max, refresh seconds, and Visual Query Builder.
- Dashboard template variables.
  - Users define variables like `$namespace`.
  - Variable values are selected from Prometheus label values.
  - Panel PromQL is stored as a template and substituted at runtime.
  - Variable definitions are currently stored in browser `localStorage`, not PostgreSQL.
- Dashboard time controls.
  - Time range selector: last 15m, 1h, 6h, 24h.
  - Auto refresh selector: off, 15s, 30s, 1m, 5m.
  - Manual refresh button and last refresh timestamp.
- AI incident summaries for alert notifications.
  - New backend file: `backend/internal/alerting/incident.go`.
  - Firing/resolved Slack and event messages include impact, likely cause, observed value, and suggested checks.
  - Specific command hints exist for kube-state-metrics, node-exporter, Prometheus, restarts, CPU, memory, deployment health, and generic metric alerts.
  - Multiline event messages render more readably in the Alerts UI.

## Runtime Layout Used During Development

The user has been running locally with Docker containers:

- Frontend: `http://localhost:5174`
- Backend: `http://localhost:18080`
- Prometheus expected locally at `http://localhost:9090`

The frontend dev container must set:

```text
VITE_BACKEND_PROXY_TARGET=http://host.docker.internal:18080
```

Otherwise Vite proxies `/api` to the wrong place inside the frontend container.

## Secrets And Environment Variables

Never commit real values for:

- `DATABASE_URL`
- `JWT_SECRET`
- `SLACK_WEBHOOK_URL`
- Supabase service role key or any Supabase secret key

For Kubernetes, create a Secret with placeholders like:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: monitoring-tool-secrets
  namespace: monitoring-tool
type: Opaque
stringData:
  DATABASE_URL: "<set-at-deploy-time>"
  JWT_SECRET: "<set-at-deploy-time>"
  SLACK_WEBHOOK_URL: "<set-at-deploy-time>"
```

Safe non-secret config can be a ConfigMap:

```yaml
PROMETHEUS_URL: "http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090"
ALLOW_ORIGIN: "*"
ALERT_EVAL_INTERVAL_SECONDS: "15"
```

Adjust `ALLOW_ORIGIN` to the actual production origin when the final URL is known.

## K3s Deployment Work To Do Next

1. Add production Dockerfiles.
   - Backend: multi-stage Go build, copy static binary into small runtime image.
   - Frontend: `npm ci`, `npm run build`, serve `dist` using nginx or Caddy.
2. Add frontend production reverse proxy config.
   - `/api` routes to backend service.
   - `/ws` routes to backend service with WebSocket upgrade support.
   - Everything else serves the SPA.
3. Add Kubernetes manifests under `k8s/`.
   - Namespace
   - ConfigMap
   - Secret template with placeholders only
   - Backend Deployment and Service
   - Frontend Deployment and Service
   - Ingress or Traefik IngressRoute
4. Decide image naming.
   - If deploying from local K3s/containerd, document import/build steps.
   - If using registry, document tags and push commands.
5. Verify in cluster.
   - Backend `/healthz`
   - Backend `/readyz`
   - Frontend loads
   - Login works
   - Prometheus queries return data
   - WebSocket live updates work
   - Slack test notification works
   - Automatic alert evaluation works

## Useful Verification Commands

Backend tests:

```powershell
docker run --rm -v "${PWD}\backend:/src" -w /src golang:1.22-alpine go test ./...
```

Frontend build:

```powershell
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

Clean generated frontend artifacts after build:

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

Secret scan before every commit:

```powershell
rg -n "hooks[.]slack[.]com|postgresql[:]//|service[_]role|SLACK_WEBHOOK_URL[=].*https|DATABASE_URL[=].*postgres|JWT_SECRET[=].*[^[:space:]]" .
```

## Suggested First Deployment Commit

Create these files:

- `backend/Dockerfile`
- `frontend/Dockerfile`
- `frontend/nginx.conf` or `frontend/Caddyfile`
- `k8s/namespace.yaml`
- `k8s/configmap.yaml`
- `k8s/secret.example.yaml`
- `k8s/backend.yaml`
- `k8s/frontend.yaml`
- `k8s/ingress.yaml`
- `docs/k3s-deployment.md`

Keep real secret values outside Git.
