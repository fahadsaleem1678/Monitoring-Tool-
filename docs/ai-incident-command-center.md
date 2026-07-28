# AI Incident Command Center

## Verify Current Services

Run on the AlmaLinux VM:

```bash
kubectl get pods -n monitoring-tool
kubectl get svc -n monitoring-tool
```

## Verify Backend

```bash
kubectl run backend-test -n monitoring-tool --rm -it --restart=Never --image=curlimages/curl -- curl -sS http://monitoring-tool-backend:8080/healthz
kubectl run backend-ready-test -n monitoring-tool --rm -it --restart=Never --image=curlimages/curl -- curl -sS http://monitoring-tool-backend:8080/readyz
```

## Build Images

Run from the project root on the AlmaLinux VM:

```bash
docker build -t monitoring-tool-backend:0.1.0 backend
docker build -t monitoring-tool-frontend:0.1.1 frontend
docker build -t incident-agent:0.1.0 incident-agent
docker build --build-arg MCP_APP=prometheus-mcp -t prometheus-mcp:0.1.0 mcp
docker build --build-arg MCP_APP=kubernetes-mcp -t kubernetes-mcp:0.1.0 mcp
```

If k3s uses containerd and cannot see Docker images directly:

```bash
docker save monitoring-tool-backend:0.1.0 monitoring-tool-frontend:0.1.1 incident-agent:0.1.0 prometheus-mcp:0.1.0 kubernetes-mcp:0.1.0 -o /tmp/monitoring-ai-images.tar
sudo k3s ctr images import /tmp/monitoring-ai-images.tar
```

## Deploy

Before applying manifests, add `AGENT_SERVICE_TOKEN` and `LLM_API_KEY` to the real `k8s/secret.yaml`.

```bash
kubectl apply -k k8s
kubectl rollout restart deploy/monitoring-tool-backend -n monitoring-tool
kubectl rollout restart deploy/monitoring-tool-frontend -n monitoring-tool
kubectl rollout status deploy/monitoring-tool-backend -n monitoring-tool
kubectl rollout status deploy/monitoring-tool-frontend -n monitoring-tool
kubectl rollout status deploy/incident-agent -n monitoring-tool
kubectl rollout status deploy/prometheus-mcp -n monitoring-tool
kubectl rollout status deploy/kubernetes-mcp -n monitoring-tool
```

## Verify AI Services

```bash
kubectl run ai-health-test -n monitoring-tool --rm -it --restart=Never --image=curlimages/curl -- sh -c 'curl -sS http://incident-agent:8090/healthz && echo && curl -sS http://prometheus-mcp:8091/healthz && echo && curl -sS http://kubernetes-mcp:8091/healthz'
```

## Verify MCP Tools

```bash
kubectl run mcp-test -n monitoring-tool --rm -it --restart=Never --image=curlimages/curl -- sh -c 'curl -sS -X POST http://prometheus-mcp:8091/tools/prometheus.query -H "Content-Type: application/json" -d "{\"query\":\"sum(up)\"}" && echo && curl -sS -X POST http://kubernetes-mcp:8091/tools/kubernetes.pods -H "Content-Type: application/json" -d "{\"namespace\":\"monitoring-tool\"}"'
```

Expected result:

- Prometheus MCP returns a series count.
- Kubernetes MCP returns pod names.
- The frontend has an `Incidents` tab after login.

## Windows Browser Access

Expose the frontend through the VM networking method already used for the current deployment, then open the frontend and select the `Incidents` tab.
