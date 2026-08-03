import os
import time
from dataclasses import dataclass
from typing import Callable

from app.backend_client import BackendClient
from app.investigator import (
    build_draft,
    build_related_promql_queries,
    choose_pod_for_logs,
    extract_alert_query,
    extract_label_value,
)
from app.llm_client import LLMClient
from app.mcp_kubernetes_client import KubernetesMCPClient
from app.mcp_prometheus_client import PrometheusMCPClient


@dataclass(frozen=True)
class SafetyLimits:
    max_prometheus_range_minutes: int = 60
    max_log_lines: int = 80

    @classmethod
    def from_env(cls) -> "SafetyLimits":
        return cls(
            max_prometheus_range_minutes=_bounded_int("MAX_PROMETHEUS_QUERY_RANGE_MINUTES", 60, 5, 60),
            max_log_lines=_bounded_int("MAX_LOG_LINES", 80, 10, 200),
        )


class InvestigationRunner:
    def __init__(
        self,
        backend: BackendClient,
        prometheus: PrometheusMCPClient,
        kubernetes: KubernetesMCPClient,
        llm: LLMClient,
        limits: SafetyLimits,
    ):
        self.backend = backend
        self.prometheus = prometheus
        self.kubernetes = kubernetes
        self.llm = llm
        self.limits = limits

    async def investigate_once(self, on_started: Callable[[], None] | None = None) -> bool:
        incidents = await self.backend.list_incidents()
        pending = [item for item in incidents if item.get("status") == "pending_investigation"]
        if not pending:
            return False
        claimed = await self.backend.claim(pending[0]["id"])
        if on_started:
            on_started()
        evidence = await self.collect_evidence(claimed)
        draft = await self.generate_draft(claimed, evidence)
        await self.backend.complete(claimed["id"], draft)
        return True

    async def collect_evidence(self, incident: dict) -> list[dict]:
        evidence = []
        alert_query = extract_alert_query(incident) or "sum(up == 0)"
        namespace = extract_label_value(alert_query, "namespace")
        queries = [alert_query] + build_related_promql_queries(alert_query)
        for query in queries[:4]:
            evidence.append(
                await self._prometheus_step("prometheus-query", query, lambda query=query: self.prometheus.instant_query(query))
            )

        end = int(time.time())
        start = end - (self.limits.max_prometheus_range_minutes * 60)
        step = max(30, int((end - start) / 60))
        evidence.append(
            await self._prometheus_step(
                "prometheus-range-query",
                f"{alert_query} [{self.limits.max_prometheus_range_minutes}m]",
                lambda: self.prometheus.range_query(alert_query, start, end, step),
            )
        )
        evidence.append(await self._prometheus_step("prometheus-alerts", "active alerts", self.prometheus.active_alerts))
        evidence.append(await self._prometheus_step("prometheus-rules", "alerting and recording rules", lambda: self.prometheus.rules(alert_query)))
        evidence.append(await self._prometheus_step("prometheus-targets", "scrape target health", self.prometheus.targets))

        pods_result = await self._safe_tool_call(lambda: self.kubernetes.pods(namespace))
        evidence.append(
            self._kubernetes_step(
                "kubernetes-pods",
                f"kubectl get pods -n {namespace}" if namespace else "kubectl get pods -A",
                pods_result,
            )
        )
        evidence.append(
            self._kubernetes_step(
                "kubernetes-events",
                f"kubectl get events -n {namespace} --sort-by=.lastTimestamp"
                if namespace
                else "kubectl get events -A --sort-by=.lastTimestamp",
                await self._safe_tool_call(lambda: self.kubernetes.events(namespace)),
            )
        )
        evidence.append(
            self._kubernetes_step(
                "kubernetes-deployments",
                f"kubectl get deployments -n {namespace}" if namespace else "kubectl get deployments -A",
                await self._safe_tool_call(lambda: self.kubernetes.deployments(namespace)),
            )
        )
        log_namespace, log_pod = choose_pod_for_logs(pods_result)
        if log_namespace and log_pod:
            evidence.append(
                self._kubernetes_step(
                    "kubernetes-logs",
                    f"kubectl logs {log_pod} -n {log_namespace} --tail={self.limits.max_log_lines}",
                    await self._safe_tool_call(lambda: self.kubernetes.logs(log_namespace, log_pod, self.limits.max_log_lines)),
                )
            )
        return evidence

    async def _prometheus_step(self, tool_name: str, query_or_command: str, call) -> dict:
        try:
            result = await call()
            return _step("promql", tool_name, query_or_command, result)
        except Exception as exc:
            return {
                "step_type": "promql",
                "tool_name": tool_name,
                "query_or_command": query_or_command,
                "result_summary": "Prometheus evidence collection failed; continuing with remaining safe checks",
                "raw_result_json": {"error": str(exc)},
            }

    async def _safe_tool_call(self, call) -> dict:
        try:
            return await call()
        except Exception as exc:
            return {
                "summary": "Evidence collection failed; continuing with remaining safe checks",
                "data": {},
                "error": str(exc),
            }

    def _kubernetes_step(self, tool_name: str, query_or_command: str, raw_result: dict) -> dict:
        return _step("kubernetes", tool_name, query_or_command, raw_result)

    async def generate_draft(self, incident: dict, evidence: list[dict]) -> dict:
        try:
            llm_draft = await self.llm.generate_incident_draft(incident, evidence)
            llm_step = {
                "step_type": "llm",
                "tool_name": f"{llm_draft['provider']}:{llm_draft['model']}",
                "query_or_command": "generate_incident_draft(incident, evidence)",
                "result_summary": llm_draft["summary"],
                "raw_result_json": {
                    "probable_cause": llm_draft["probable_cause"],
                    "evidence_summary": llm_draft["evidence_summary"],
                    "suggested_next_checks": llm_draft["suggested_next_checks"],
                    "slack_message": llm_draft["slack_message"],
                },
            }
            return {
                "summary": llm_draft["summary"],
                "confidence": llm_draft["confidence"],
                "draft_message": llm_draft["slack_message"],
                "steps": evidence + [llm_step],
            }
        except Exception as exc:
            fallback = build_draft(incident, evidence)
            fallback["steps"] = evidence + [
                {
                    "step_type": "llm",
                    "tool_name": f"{self.llm.provider}:{self.llm.model}",
                    "query_or_command": "generate_incident_draft(incident, evidence)",
                    "result_summary": "LLM draft generation failed; deterministic fallback draft was used",
                    "raw_result_json": {"error": str(exc)},
                }
            ]
            return fallback


def _step(step_type: str, tool_name: str, query_or_command: str, raw_result: dict) -> dict:
    return {
        "step_type": step_type,
        "tool_name": tool_name,
        "query_or_command": query_or_command,
        "result_summary": raw_result.get("summary", "tool call completed"),
        "raw_result_json": raw_result,
    }


def _bounded_int(key: str, fallback: int, minimum: int, maximum: int) -> int:
    try:
        value = int(os.getenv(key, str(fallback)))
    except ValueError:
        return fallback
    return min(max(value, minimum), maximum)
