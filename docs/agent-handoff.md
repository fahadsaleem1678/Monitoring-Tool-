# Agent Handoff

## Current Snapshot

Latest pushed commit:

```text
0a6156d fix: default mcp image to kubernetes app
```

Recent important commits:

```text
af0ac34 feat: add namespace assistant tool
c1e68b2 fix: fallback priority summary when pod listing times out
2a01157 feat: add optional llm routing for assistant
0219498 feat: summarize priority cluster issues
903faed fix: keep pod context from metric answers
```

Before changing anything:

```powershell
cd "E:\Internship\Monitoring tool"
git status --short
```

The Cluster Assistant now has:

```text
deterministic checks first
optional LLM routing second
read-only backend-owned tool execution only
```

The LLM is not a free-form Kubernetes operator. It routes vague natural-language questions to approved read-only intents, and the backend runs real Prometheus/Kubernetes checks.

Known working assistant state:

```text
tell me all the namespaces in my cluster -> deterministic namespaces tool
is anything repeatedly dying? -> LLM routed -> pod_crashloops
```

Known good OpenRouter model for the assistant router:

```text
nvidia/nemotron-3-nano-30b-a3b:free
```

Models/configs that caused trouble:

```text
openrouter/free returned empty message.content
openai/gpt-oss-20b:free returned empty message.content
meta-llama/llama-3.2-3b-instruct:free returned HTTP 404 unavailable for free
```

Assistant LLM backend config should be:

```text
ASSISTANT_LLM_ENABLED=true
LLM_PROVIDER=openai
OPENAI_BASE_URL=https://openrouter.ai/api/v1
OPENAI_MODEL=nvidia/nemotron-3-nano-30b-a3b:free
OPENAI_TIMEOUT_SECONDS=60
OPENAI_API_KEY=present in monitoring-tool-secrets
```

Check backend sees it:

```bash
kubectl -n monitoring-tool exec deployment/monitoring-tool-backend -- sh -c '
echo ASSISTANT_LLM_ENABLED=$ASSISTANT_LLM_ENABLED
echo LLM_PROVIDER=$LLM_PROVIDER
echo OPENAI_BASE_URL=$OPENAI_BASE_URL
echo OPENAI_MODEL=$OPENAI_MODEL
echo OPENAI_TIMEOUT_SECONDS=$OPENAI_TIMEOUT_SECONDS
test -n "$OPENAI_API_KEY" && echo OPENAI_API_KEY=present || echo OPENAI_API_KEY=missing
'
```

Debug assistant LLM routing:

```bash
kubectl logs -n monitoring-tool deployment/monitoring-tool-backend --tail=200 | grep 'assistant llm'
```

Relevant log messages:

```text
assistant llm route retrying without response_format
assistant llm route failed
assistant llm route parse failed
```

The OpenRouter key was pasted in chat during debugging. Treat it as exposed and rotate it.

To write a new key without losing other secret values:

```bash
read -s OPENAI_API_KEY

kubectl -n monitoring-tool patch secret monitoring-tool-secrets \
  --type merge \
  -p "{\"data\":{\"OPENAI_API_KEY\":\"$(printf %s "$OPENAI_API_KEY" | base64 -w0)\"}}"
```

Important:

```text
read -s OPENAI_API_KEY only stores a shell variable. It does not write anything to Kubernetes until the kubectl patch secret command is run.
```

Verify key exists:

```bash
kubectl -n monitoring-tool get secret monitoring-tool-secrets \
  -o jsonpath='{.data.OPENAI_API_KEY}' | wc -c
```

Restart backend after secret/config changes:

```bash
kubectl rollout restart deployment/monitoring-tool-backend -n monitoring-tool
kubectl rollout status deployment/monitoring-tool-backend -n monitoring-tool --timeout=120s
```

## Cluster Assistant Tool Catalog

Current approved read-only assistant capabilities:

```text
pod health
crash loops
image pull errors
pending pods
pod restarts
node readiness
running pod count
Prometheus scrape targets
list all pods in monitoring-tool namespace
list unhealthy pods
pod details/logs/events
priority cluster summary
list namespaces
```

Important files for the assistant:

```text
backend/internal/chat/service.go
backend/internal/chat/llm_router.go
backend/internal/chat/service_test.go
backend/internal/kubernetesmcp/client.go
backend/cmd/server/main.go
frontend/src/components/assistant/AssistantView.tsx
frontend/src/api/chat.ts
mcp/kubernetes-mcp/app.py
mcp/Dockerfile
k8s/kubernetes-mcp.yaml
k8s/configmap.yaml
k8s/secret.example.yaml
```

Next best tool-expansion work:

```text
list deployments
list services
list nodes
list pods across all namespaces
describe deployment health
show recent namespace events
summarize workload by namespace
```

Avoid write actions for now:

```text
delete pod
restart deployment
scale deployment
patch resources
exec into pod
apply YAML
send Slack automatically
```

## Kubernetes MCP Current Notes

`mcp/Dockerfile` now defaults to the Kubernetes app:

```text
ARG MCP_APP=kubernetes-mcp
COPY ${MCP_APP}/ ./service/
CMD ["uvicorn", "service.app:app", "--host", "0.0.0.0", "--port", "8091"]
```

Previous failure:

```text
ERROR: Error loading ASGI app. Could not import module "service.app".
```

Cause:

```text
kubernetes-mcp image was built without a correct MCP_APP build arg / app copy.
```

Fix commit:

```text
0a6156d fix: default mcp image to kubernetes app
```

Kubernetes MCP exposes:

```text
/tools/kubernetes.pods
/tools/kubernetes.namespaces
/tools/kubernetes.events
/tools/kubernetes.deployments
/tools/kubernetes.logs
```

Test namespace endpoint:

```bash
kubectl -n monitoring-tool port-forward svc/kubernetes-mcp 8091:8091
```

In another Alma terminal:

```bash
curl -s -X POST http://127.0.0.1:8091/tools/kubernetes.namespaces \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Check namespace RBAC:

```bash
kubectl auth can-i list namespaces \
  --as=system:serviceaccount:monitoring-tool:kubernetes-mcp
```

Expected:

```text
yes
```

## Current Deploy Commands

From Windows PowerShell:

```powershell
cd "E:\Internship\Monitoring tool"

docker build -t kubernetes-mcp:0.1.0 .\mcp
docker save kubernetes-mcp:0.1.0 -o kubernetes-mcp-fixed.tar

docker build -t monitoring-tool-backend:0.1.0 .\backend
docker save monitoring-tool-backend:0.1.0 -o monitoring-tool-backend-latest.tar

docker build -t monitoring-tool-frontend:0.1.1 .\frontend
docker save monitoring-tool-frontend:0.1.1 -o monitoring-tool-frontend-latest.tar

tar -czf monitoring-tool-k8s-latest.tar.gz k8s

scp -P 4444 .\kubernetes-mcp-fixed.tar fahad@127.0.0.1:/tmp/kubernetes-mcp-fixed.tar
scp -P 4444 .\monitoring-tool-backend-latest.tar fahad@127.0.0.1:/tmp/monitoring-tool-backend-latest.tar
scp -P 4444 .\monitoring-tool-frontend-latest.tar fahad@127.0.0.1:/tmp/monitoring-tool-frontend-latest.tar
scp -P 4444 .\monitoring-tool-k8s-latest.tar.gz fahad@127.0.0.1:/tmp/monitoring-tool-k8s-latest.tar.gz
```

On Alma:

```bash
sudo k3s ctr -n k8s.io images import /tmp/kubernetes-mcp-fixed.tar
sudo k3s ctr -n k8s.io images import /tmp/monitoring-tool-backend-latest.tar
sudo k3s ctr -n k8s.io images import /tmp/monitoring-tool-frontend-latest.tar

rm -rf ~/monitoring-tool-deploy
mkdir -p ~/monitoring-tool-deploy
tar -xzf /tmp/monitoring-tool-k8s-latest.tar.gz -C ~/monitoring-tool-deploy

kubectl apply -k ~/monitoring-tool-deploy/k8s

kubectl rollout restart deployment/kubernetes-mcp -n monitoring-tool
kubectl rollout restart deployment/monitoring-tool-backend -n monitoring-tool
kubectl rollout restart deployment/monitoring-tool-frontend -n monitoring-tool

kubectl rollout status deployment/kubernetes-mcp -n monitoring-tool --timeout=120s
kubectl rollout status deployment/monitoring-tool-backend -n monitoring-tool --timeout=120s
kubectl rollout status deployment/monitoring-tool-frontend -n monitoring-tool --timeout=120s
```

If `kubernetes-mcp` rollout gets stuck:

```bash
kubectl get pods -n monitoring-tool | grep kubernetes-mcp
kubectl delete pod -n monitoring-tool -l app=kubernetes-mcp --force --grace-period=0
kubectl rollout restart deployment/kubernetes-mcp -n monitoring-tool
kubectl rollout status deployment/kubernetes-mcp -n monitoring-tool --timeout=120s
```

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
