# LLM and MCP Integration Plan

## Goal

Upgrade the AI incident review workflow so the incident agent uses an LLM plus richer Prometheus and Kubernetes MCP servers to investigate alerts, collect evidence, and generate human-reviewed incident drafts.

The system should stay safe by default:

- The LLM does not directly mutate infrastructure.
- Prometheus destructive/admin tools remain disabled.
- Kubernetes access stays read-only.
- Slack messages still require human approval.
- Raw evidence stays visible in the incident review UI.

## Target Architecture

```text
React UI
  |
  v
Go Backend
  |
  v
Incident Agent
  |
  +-- LLM Provider
  |     OpenAI gpt-4.1-mini or local Ollama
  |
  +-- Prometheus MCP
  |     prometheus/prometheus-mcp
  |
  +-- Kubernetes MCP
        containers/kubernetes-mcp-server
```

The Go backend and React UI remain the product surface. The incident agent remains the orchestration layer. MCP servers provide infrastructure evidence, and the LLM turns that evidence into a clear draft.

## Recommended MCP Servers

### Prometheus

Use:

```text
prometheus/prometheus-mcp
```

Useful capabilities:

- Instant PromQL queries.
- Range PromQL queries.
- Active alerts.
- Alerting and recording rules.
- Scrape targets.
- Label names and label values.
- Metric metadata.
- Runtime and TSDB inspection.

Keep dangerous TSDB admin tools disabled.

### Kubernetes

Use:

```text
containers/kubernetes-mcp-server
```

Run it in a restricted mode:

```bash
kubernetes-mcp-server \
  --read-only \
  --disable-multi-cluster \
  --toolsets core
```

Keep the Kubernetes ServiceAccount read-only through RBAC.

Useful capabilities:

- List and inspect pods.
- Read pod logs.
- Read recent events.
- Inspect deployments and workloads.
- Inspect nodes.
- Check resource usage if available.

Avoid mutation tools such as delete, apply, scale, patch, and exec for the incident agent demo.

## LLM Provider Plan

Support two LLM modes.

### OpenAI Mode

Use this for the best demo quality:

```env
LLM_PROVIDER=openai
OPENAI_API_KEY=...
OPENAI_MODEL=gpt-4.1-mini
```

### Local Ollama Mode

Use this for a free local demo:

```env
LLM_PROVIDER=ollama
OLLAMA_URL=http://host.docker.internal:11434
OLLAMA_MODEL=qwen2.5:7b
```

The incident agent should hide provider details behind one internal interface:

```text
generate_incident_draft(incident, evidence) -> draft
```

## Investigation Flow

For each pending incident:

1. Claim one pending incident from the backend.
2. Extract alert name, severity, namespace, labels, and PromQL query.
3. Use Prometheus MCP to collect:
   - Current alert query result.
   - Range query trend for the last 30 to 60 minutes.
   - Active alerts.
   - Relevant rule details.
   - Scrape target health.
4. Use Kubernetes MCP to collect:
   - Pods in the alert namespace.
   - Unhealthy pod details.
   - Recent namespace events.
   - Deployment or workload status.
   - Logs from the most suspicious pod.
5. Build a compact evidence bundle.
6. Ask the LLM to generate:
   - Probable cause.
   - Confidence.
   - Evidence summary.
   - Suggested next checks.
   - Slack incident draft.
7. Store the draft and raw evidence in the backend.
8. Require a human to approve, reject, or regenerate the draft.

## Suggested Agent Files

Add or refactor these files under `incident-agent/app`:

```text
llm_client.py
mcp_prometheus_client.py
mcp_kubernetes_client.py
investigation_runner.py
```

Responsibilities:

- `llm_client.py`: OpenAI and Ollama provider abstraction.
- `mcp_prometheus_client.py`: Prometheus MCP tool calls.
- `mcp_kubernetes_client.py`: Kubernetes MCP tool calls.
- `investigation_runner.py`: Controlled investigation workflow.

The LLM should not choose arbitrary cluster operations. The agent should run a known sequence of safe checks and then ask the LLM to summarize.

## UI Improvements

Update the incident review UI to show:

- AI summary.
- Probable cause.
- Confidence.
- Evidence trail.
- Prometheus queries used.
- Kubernetes checks used.
- Draft Slack message.
- Approve, reject, and regenerate actions.

The evidence trail is important because it makes the AI output explainable during the demo.

## Safety Controls

Required safeguards:

- Kubernetes MCP runs read-only.
- Kubernetes RBAC grants only read/list/get and pod log access.
- Prometheus TSDB admin tools remain disabled.
- The agent enforces max Prometheus query range.
- The agent enforces max log lines.
- The agent stores raw MCP evidence.
- The LLM cannot call arbitrary tools directly.
- Slack posting requires human approval.
- Secrets are stored in Kubernetes Secret, not ConfigMap.

## Recommended Build Order

1. Add the LLM client abstraction to the incident agent.
2. Keep the current custom Prometheus and Kubernetes MCP services working.
3. Generate real LLM drafts from the existing evidence.
4. Add config for OpenAI and Ollama modes.
5. Add tests for prompt construction and draft parsing.
6. Add the official Prometheus MCP service in a separate manifest.
7. Teach the incident agent to call richer Prometheus MCP tools.
8. Add range query, active alert, rule, and target evidence.
9. Add the official Kubernetes MCP service in read-only mode.
10. Teach the incident agent to call richer Kubernetes MCP tools.
11. Add pod detail, deployment status, node/resource, event, and log evidence.
12. Improve the incident review UI evidence trail.
13. Add safety limits for query range, logs, and tool selection.
14. Run the full local demo with a generated failing pod or test alert.
15. Document setup, environment variables, and verification commands.

## Demo Scenario

Use a controlled incident:

1. Trigger a pod crash or test alert.
2. Prometheus records the alert.
3. The backend creates an incident review.
4. The incident agent collects Prometheus and Kubernetes evidence through MCP.
5. The LLM generates a draft.
6. The UI shows the draft and evidence trail.
7. A human approves the Slack message.

## Implementation Strategy

Start with the LLM integration before replacing MCP servers. This keeps the project working at each step:

```text
Current custom MCP evidence
  -> LLM-generated draft
  -> richer Prometheus MCP
  -> richer Kubernetes MCP
  -> better UI evidence trail
```

This avoids a large rewrite and gives a usable demo after the first milestone.
