# Agent Handoff

## Project

Monitoring Tool is a self-hosted Kubernetes monitoring and incident-response demo.

Core flow:

1. Backend evaluates alert rules against Prometheus.
2. A firing alert creates an incident review.
3. Incident agent collects read-only evidence through MCP servers.
4. LLM generates a human-reviewable incident summary and Slack draft.
5. Admin approves/rejects/regenerates/broadcasts from the frontend.

The LLM does not mutate infrastructure. Kubernetes MCP is read-only. Slack broadcast for firing incidents requires approval.

## Current Repo State

Last pushed commit:

```text
c7e55e6 Add LLM incident MCP integrations
```

Known uncommitted work after that commit:

- Ollama/OpenRouter LLM timeout/debug changes.
- Official Kubernetes MCP pod-list parsing fix.
- Frontend MCP source badge.
- Updated image tar artifacts may exist locally but are ignored.

Before committing again, run:

```powershell
git status --short
```

## Important Files

```text
docs/llm-mcp-integration-plan.md
docs/llm-mcp-next-steps.md
incident-agent/app/main.py
incident-agent/app/investigation_runner.py
incident-agent/app/llm_client.py
incident-agent/app/mcp_prometheus_client.py
incident-agent/app/mcp_kubernetes_client.py
frontend/src/components/incidents/IncidentReviewsView.tsx
frontend/src/styles.css
k8s/configmap.yaml
k8s/prometheus-mcp-official.yaml
k8s/kubernetes-mcp-official.yaml
```

## Cluster

Alma VM/k3s namespace:

```text
monitoring-tool
```

Useful copy pattern from Windows to Alma:

```powershell
scp -P 4444 .\incident-agent-official-kubernetes-mcp.tar fahad@127.0.0.1:/tmp/incident-agent-official-kubernetes-mcp.tar
scp -P 4444 .\monitoring-tool-frontend-0.1.1.tar fahad@127.0.0.1:/tmp/monitoring-tool-frontend-0.1.1.tar
scp -P 4444 .\monitoring-tool-k8s-official-kubernetes.tar.gz fahad@127.0.0.1:/tmp/monitoring-tool-k8s-official-kubernetes.tar.gz
```

Import images on Alma:

```bash
sudo k3s ctr -n k8s.io images import /tmp/incident-agent-official-kubernetes-mcp.tar
sudo k3s ctr -n k8s.io images import /tmp/monitoring-tool-frontend-0.1.1.tar
```

Apply manifests:

```bash
mkdir -p ~/monitoring-tool-deploy
tar -xzf /tmp/monitoring-tool-k8s-official-kubernetes.tar.gz -C ~/monitoring-tool-deploy
kubectl apply -k ~/monitoring-tool-deploy/k8s
```

Restart:

```bash
kubectl rollout restart deployment/incident-agent -n monitoring-tool
kubectl rollout restart deployment/monitoring-tool-frontend -n monitoring-tool
kubectl rollout status deployment/incident-agent -n monitoring-tool
kubectl rollout status deployment/monitoring-tool-frontend -n monitoring-tool
```

## Current Expected Pods

```bash
kubectl get pods -n monitoring-tool
```

Expected services include:

```text
incident-agent
kubernetes-mcp
kubernetes-mcp-official
monitoring-tool-backend
monitoring-tool-frontend
prometheus-mcp
prometheus-mcp-official
```

## MCP Modes

Check active modes:

```bash
kubectl get configmap monitoring-tool-config -n monitoring-tool \
  -o jsonpath='Prometheus MCP: {.data.PROMETHEUS_MCP_MODE}{"\n"}Kubernetes MCP: {.data.KUBERNETES_MCP_MODE}{"\n"}'
```

Switch both to official:

```bash
kubectl -n monitoring-tool patch configmap monitoring-tool-config \
  --type merge \
  -p '{"data":{"PROMETHEUS_MCP_MODE":"official","KUBERNETES_MCP_MODE":"official"}}'

kubectl rollout restart deployment/incident-agent -n monitoring-tool
kubectl rollout status deployment/incident-agent -n monitoring-tool
```

Debug endpoints require port-forward:

```bash
kubectl -n monitoring-tool port-forward svc/incident-agent 8090:8090
```

Then:

```bash
curl http://localhost:8090/healthz
curl http://localhost:8090/debug/prometheus-mcp
curl http://localhost:8090/debug/kubernetes-mcp
curl http://localhost:8090/debug/llm
```

Expected official evidence tool names:

```text
official-prometheus.execute_query
official-kubernetes.pods_list_in_namespace
official-kubernetes.pods_log
```

Frontend evidence should show MCP source badges:

```text
Official MCP
MVP fallback
MVP MCP
LLM
```

## LLM Status

Local Ollama on Alma is installed and reachable, but CPU-only generation timed out.

Observed:

```text
OLLAMA_URL=http://10.0.2.3:11434
OLLAMA_MODEL=llama3.2:1b
/debug/llm -> ReadTimeout
```

Ollama tags worked:

```bash
curl http://10.0.2.3:11434/api/tags
```

Current practical recommendation:

- Use OpenRouter free/OpenAI-compatible mode if demo needs real LLM text.
- Or keep deterministic fallback and emphasize MCP evidence collection.

OpenRouter config shape:

```bash
kubectl -n monitoring-tool patch configmap monitoring-tool-config \
  --type merge \
  -p '{"data":{"LLM_PROVIDER":"openai","OPENAI_BASE_URL":"https://openrouter.ai/api/v1","OPENAI_MODEL":"openrouter/free","OPENAI_TIMEOUT_SECONDS":"90","MAX_LLM_EVIDENCE_CHARS":"4000"}}'

kubectl rollout restart deployment/incident-agent -n monitoring-tool
```

Secret key expected env name:

```text
OPENAI_API_KEY
```

If an API key was pasted in chat, treat it as exposed and rotate it.

## Test Scenarios

### CrashLoopBackOff

```bash
kubectl delete deployment broken-api -n monitoring-tool --ignore-not-found

kubectl create deployment broken-api -n monitoring-tool \
  --image=busybox:1.36 \
  -- /bin/sh -c 'echo "booting broken-api"; sleep 2; echo "fatal config missing"; exit 1'

kubectl scale deployment broken-api -n monitoring-tool --replicas=2
```

Useful alert query:

```promql
sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"}) + sum(increase(kube_pod_container_status_restarts_total{namespace="monitoring-tool"}[5m]))
```

### Bad Image Pull

```bash
kubectl delete deployment bad-image-api -n monitoring-tool --ignore-not-found
kubectl create deployment bad-image-api -n monitoring-tool --image=busybox:not-a-real-tag
```

Alert query:

```promql
sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason=~"ImagePullBackOff|ErrImagePull"})
```

### Pending / Unschedulable

```bash
kubectl delete deployment oversized-api -n monitoring-tool --ignore-not-found

kubectl create deployment oversized-api -n monitoring-tool \
  --image=busybox:1.36 \
  -- /bin/sh -c 'sleep 3600'

kubectl patch deployment oversized-api -n monitoring-tool \
  --type='json' \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/resources","value":{"requests":{"cpu":"100","memory":"100Gi"}}}]'
```

Alert query:

```promql
sum(kube_pod_status_phase{namespace="monitoring-tool",phase="Pending"})
```

## Known Gotchas

- No-data is not treated as resolved while an alert is open. This prevents false resolved Slack alerts but can keep old test alerts open.
- If a new broken-api incident is not created, the old alert event may still be firing. Create a new alert rule name or delete/recreate the rule for testing.
- Applying k8s manifests can reset ConfigMap values; re-check MCP modes and LLM settings afterward.
- Official MCPs can return large payloads; agent compacts evidence before saving.
- Tars are intentionally ignored by git.

## Validation Commands

Agent tests:

```powershell
python -m pytest incident-agent\tests
```

Frontend build:

```powershell
docker run --rm -v "${PWD}\frontend:/app" -w /app node:20-alpine npm run build
```

Kustomize:

```powershell
kubectl kustomize k8s
```

Build deploy images:

```powershell
docker build -t incident-agent:0.1.0 .\incident-agent
docker save incident-agent:0.1.0 -o incident-agent-official-kubernetes-mcp.tar

docker build -t monitoring-tool-frontend:0.1.1 .\frontend
docker save monitoring-tool-frontend:0.1.1 -o monitoring-tool-frontend-0.1.1.tar
```

## Demo Pitch

Say:

```text
We intentionally break a Kubernetes workload. The monitoring tool detects the alert from Prometheus, collects real read-only evidence through official Prometheus and Kubernetes MCP servers, asks an LLM to summarize it when available, and presents a human-reviewable Slack draft with full evidence and audit history.
```

Safety point:

```text
The LLM is not an autonomous operator. It cannot delete pods, patch deployments, scale workloads, or send Slack alerts without approval. It only summarizes evidence from safe read-only tools.
```
