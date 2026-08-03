# LLM and MCP Integration: Next Steps

## Current Status

The LLM incident workflow is now working end-to-end with local Ollama and official Prometheus MCP.

Validated in the Alma k3s cluster:

- Incident agent runs in `PROMETHEUS_MCP_MODE=official`.
- Official Prometheus MCP answers agent calls at `http://prometheus-mcp-official:8080/mcp`.
- CrashLoopBackOff incident investigation completes.
- Frontend evidence trail shows official MCP tool usage such as `official-prometheus.execute_query`.
- Official MCP raw evidence is compacted before storage so investigation completion no longer times out.
- Resolved Slack alerts are guarded against Prometheus no-data false recovery.

## Completed Plan Items

- Add LLM client abstraction.
- Support local Ollama mode.
- Keep MVP Prometheus and Kubernetes MCP services working.
- Generate LLM incident drafts from collected evidence.
- Add tests for LLM prompt and draft parsing.
- Add official Prometheus MCP service in a separate manifest.
- Teach the incident agent to call official Prometheus MCP tools.
- Add Prometheus range query, alert, rule, metadata, and target evidence.
- Improve the incident review UI evidence trail enough to verify MCP source.
- Add safety limits for Prometheus query range and Kubernetes logs.
- Run a full demo with a generated failing pod.

## Current Implementation Update

Official Kubernetes MCP has now been added in parallel with the current MVP Kubernetes MCP, the same way official Prometheus MCP was added.

Implemented:

- Separate `kubernetes-mcp-official` deployment and service.
- Dedicated read-only ServiceAccount, ClusterRole, and ClusterRoleBinding.
- `KUBERNETES_MCP_MODE=mvp|official`.
- `OFFICIAL_KUBERNETES_MCP_URL`.
- `OFFICIAL_KUBERNETES_MCP_TIMEOUT_SECONDS`.
- Official Kubernetes MCP client.
- Fallback from official Kubernetes MCP to MVP Kubernetes MCP.
- Debug endpoints:

```text
/debug/kubernetes-mcp
/debug/kubernetes-mcp-official
```

- Compact official Kubernetes MCP evidence before saving.
- Tests for official Kubernetes response parsing and fallback.

Still required:

1. Deploy the new manifests and incident-agent image to Alma.
2. Confirm `kubernetes-mcp-official` pod starts successfully.
3. Run `/debug/kubernetes-mcp-official`.
4. Switch `KUBERNETES_MCP_MODE=official`.
5. Re-run CrashLoopBackOff investigation.
6. Verify evidence tool names such as `official-kubernetes.resources_list` and `official-kubernetes.pods_log`.

## Why This Is Next

The original implementation plan moves from:

```text
LLM-generated draft
  -> richer Prometheus MCP
  -> richer Kubernetes MCP
  -> better UI evidence trail
```

We completed the richer Prometheus MCP step first. The official Kubernetes MCP integration is now implemented locally and is the next cluster validation target.

## Proposed Official Kubernetes MCP Scope

Keep the official Kubernetes MCP read-only for now.

Required capabilities:

- List pods in namespace.
- Inspect pod status and container waiting reasons.
- Read pod logs with a max-line limit.
- List namespace events.
- List deployments and workload status.
- Optionally inspect nodes and resource usage if the server supports it safely.

Not allowed:

- Delete.
- Apply.
- Patch.
- Scale.
- Exec into containers.
- Any multi-cluster access.

## Deployment Work Items

1. Copy the new k8s manifest archive to Alma.
2. Apply manifests.
3. Import the new incident-agent image.
4. Restart `incident-agent`.
5. Run debug smoke checks.
6. Switch to official Kubernetes MCP mode.
7. Run CrashLoopBackOff demo.

Config keys:

```text
KUBERNETES_MCP_MODE=mvp
OFFICIAL_KUBERNETES_MCP_URL=http://kubernetes-mcp-official:<port>
OFFICIAL_KUBERNETES_MCP_TIMEOUT_SECONDS=5
```

## Verification Commands

Check current Prometheus MCP mode:

```bash
kubectl get configmap monitoring-tool-config -n monitoring-tool \
  -o jsonpath='{.data.PROMETHEUS_MCP_MODE}{"\n"}'
```

Confirm official Prometheus MCP usage:

```bash
kubectl logs deployment/incident-agent -n monitoring-tool --tail=200 | grep prometheus-mcp
```

Expected:

```text
POST http://prometheus-mcp-official:8080/mcp "HTTP/1.1 200 OK"
```

Frontend evidence should show:

```text
official-prometheus.execute_query
```

## Small Cleanup Before Kubernetes MCP

- Rename Prometheus summaries from `0 vector` to `0 series`.
- Remove or hide very noisy raw text previews in the frontend if the evidence panel feels too dense.
- Consider adding a small `MCP source` badge in the evidence trail: `MVP` or `Official`.
- Keep MVP fallback enabled until official Kubernetes MCP is proven through the same CrashLoopBackOff test.
