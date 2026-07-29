import asyncio
import os

from fastapi import FastAPI
from prometheus_client import Counter, Histogram, generate_latest
from starlette.responses import Response

from app.backend_client import BackendClient
from app.investigator import build_draft, build_related_promql_queries, choose_pod_for_logs, extract_alert_query, extract_label_value
from app.mcp_clients import KubernetesMCPClient, PrometheusMCPClient

app = FastAPI()

investigations_started = Counter("incident_agent_investigations_started_total", "Investigations started")
investigations_completed = Counter("incident_agent_investigations_completed_total", "Investigations completed")
investigations_failed = Counter("incident_agent_investigations_failed_total", "Investigations failed")
investigation_duration = Histogram("incident_agent_investigation_duration_seconds", "Investigation duration")


@app.get("/healthz")
async def healthz():
    return {"status": "healthy", "service": "incident-agent"}


@app.get("/metrics")
async def metrics():
    return Response(generate_latest(), media_type="text/plain; version=0.0.4")


async def investigate_once():
    backend = BackendClient(os.environ["BACKEND_URL"], os.getenv("AGENT_SERVICE_TOKEN", "dev-agent-token"))
    prometheus = PrometheusMCPClient(os.environ["PROMETHEUS_MCP_URL"])
    kubernetes = KubernetesMCPClient(os.environ["KUBERNETES_MCP_URL"])
    incidents = await backend.list_incidents()
    pending = [item for item in incidents if item.get("status") == "pending_investigation"]
    for incident in pending[:1]:
        investigations_started.inc()
        with investigation_duration.time():
            claimed = await backend.claim(incident["id"])
            evidence = []
            try:
                alert_query = extract_alert_query(claimed) or "sum(up == 0)"
                namespace = extract_label_value(alert_query, "namespace")
                queries = [alert_query] + build_related_promql_queries(alert_query)
                for query in queries[:4]:
                    prom_result = await prometheus.instant_query(query)
                    evidence.append(
                        {
                            "step_type": "promql",
                            "tool_name": "prometheus",
                            "query_or_command": query,
                            "result_summary": prom_result.get("summary", "Prometheus query completed"),
                            "raw_result_json": prom_result,
                        }
                    )
                pods_result = await kubernetes.pods(namespace)
                evidence.append(
                    {
                        "step_type": "kubernetes",
                        "tool_name": "kubernetes-pods",
                        "query_or_command": f"kubectl get pods -n {namespace}" if namespace else "kubectl get pods -A",
                        "result_summary": pods_result.get("summary", "Kubernetes pod query completed"),
                        "raw_result_json": pods_result,
                    }
                )
                events_result = await kubernetes.events(namespace)
                evidence.append(
                    {
                        "step_type": "kubernetes",
                        "tool_name": "kubernetes-events",
                        "query_or_command": f"kubectl get events -n {namespace} --sort-by=.lastTimestamp"
                        if namespace
                        else "kubectl get events -A --sort-by=.lastTimestamp",
                        "result_summary": events_result.get("summary", "Kubernetes event query completed"),
                        "raw_result_json": events_result,
                    }
                )
                log_namespace, log_pod = choose_pod_for_logs(pods_result)
                if log_namespace and log_pod:
                    logs_result = await kubernetes.logs(log_namespace, log_pod)
                    evidence.append(
                        {
                            "step_type": "kubernetes",
                            "tool_name": "kubernetes-logs",
                            "query_or_command": f"kubectl logs {log_pod} -n {log_namespace} --tail=80",
                            "result_summary": logs_result.get("summary", "Kubernetes log query completed"),
                            "raw_result_json": logs_result,
                        }
                    )
                await backend.complete(claimed["id"], build_draft(claimed, evidence))
                investigations_completed.inc()
            except Exception:
                investigations_failed.inc()
                raise


async def loop_forever():
    interval = int(os.getenv("AGENT_POLL_INTERVAL_SECONDS", "15"))
    while True:
        try:
            await investigate_once()
        except Exception:
            pass
        await asyncio.sleep(interval)


@app.on_event("startup")
async def startup():
    asyncio.create_task(loop_forever())
