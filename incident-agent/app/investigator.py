import re


def extract_alert_query(incident: dict) -> str:
    for field in ("summary", "draft_message"):
        value = str(incident.get(field, ""))
        match = re.search(r"Query:\s*`([^`]+)`", value)
        if match:
            return match.group(1).strip()
    return ""


def extract_label_value(query: str, label: str) -> str:
    match = re.search(rf'{re.escape(label)}\s*(?:=|!=|=~|!~)\s*"([^"]+)"', query)
    if not match:
        return ""
    value = match.group(1).strip()
    if any(token in value for token in ("$", "*", ".")):
        return ""
    return value


def build_related_promql_queries(query: str) -> list[str]:
    namespace = extract_label_value(query, "namespace")
    namespace_selector = f'{{namespace="{namespace}"}}' if namespace else ""
    lower = query.lower()
    related = []

    if "crashloopbackoff" in lower or "waiting_reason" in lower:
        related.append(
            f'sum by (namespace, pod, container, reason) (kube_pod_container_status_waiting_reason{namespace_selector})'
        )
        related.append(
            f'sum by (namespace, pod, container) (increase(kube_pod_container_status_restarts_total{namespace_selector}[10m]))'
        )
    elif "restart" in lower:
        related.append(
            f'sum by (namespace, pod, container) (increase(kube_pod_container_status_restarts_total{namespace_selector}[10m]))'
        )
        related.append(f'sum by (namespace, pod) (kube_pod_status_phase{namespace_selector})')
    elif "up" in lower:
        related.append("sum by (namespace, job, instance) (up == 0)")
        related.append("sum by (namespace, job) (up)")
    else:
        related.append(f'sum by (namespace, pod) (kube_pod_status_phase{namespace_selector})')

    return [item for item in related if item != query]


def choose_pod_for_logs(pods_result: dict) -> tuple[str, str]:
    pods = pods_result.get("data", {}).get("pods", [])
    for pod in pods:
        waiting_reasons = pod.get("waiting_reasons", [])
        if waiting_reasons:
            return pod.get("namespace", ""), pod.get("name", "")
    for pod in pods:
        if pod.get("restart_count", 0) > 0:
            return pod.get("namespace", ""), pod.get("name", "")
    for pod in pods:
        if pod.get("phase") not in ("Running", "Succeeded"):
            return pod.get("namespace", ""), pod.get("name", "")
    return "", ""


def summarize_findings(evidence: list[dict]) -> str:
    signals = []
    for item in evidence:
        summary = item.get("result_summary", "")
        command = item.get("query_or_command", "")
        if "CrashLoopBackOff" in str(item.get("raw_result_json", "")) or "CrashLoopBackOff" in command:
            signals.append("CrashLoopBackOff state detected")
        if "restart" in command.lower():
            signals.append("recent container restart activity checked")
        if item.get("tool_name") == "kubernetes-events":
            signals.append("namespace events inspected")
        if item.get("tool_name") == "kubernetes-logs":
            signals.append("pod logs inspected")
        if summary:
            signals.append(summary)
    unique = []
    for signal in signals:
        if signal not in unique:
            unique.append(signal)
    return "; ".join(unique[:4]) if unique else "No supporting evidence was collected."


def build_draft(incident: dict, evidence: list[dict]) -> dict:
    title = incident.get("title", "Monitoring incident")
    severity = incident.get("severity", "warning")
    alert_query = extract_alert_query(incident)
    namespace = extract_label_value(alert_query, "namespace")
    finding_summary = summarize_findings(evidence)
    scope = f" in namespace {namespace}" if namespace else ""
    summary = f"{title}{scope}: {finding_summary}"
    evidence_lines = "\n".join(
        f"- {item.get('tool_name', 'tool')}: `{item.get('query_or_command', '')}` -> {item.get('result_summary', '')}"
        for item in evidence
    )
    suggested_checks = [
        f"kubectl get pods -n {namespace}" if namespace else "kubectl get pods -A",
        f"kubectl get events -n {namespace} --sort-by=.lastTimestamp" if namespace else "kubectl get events -A --sort-by=.lastTimestamp",
    ]
    if namespace:
        suggested_checks.append(f"kubectl logs <pod> -n {namespace} --tail=100")
    draft = (
        f":rotating_light: Monitoring Tool incident draft: {title}\n"
        f"Severity: {severity}\n"
        f"Summary: {summary}\n"
        f"Alert query: `{alert_query or 'unknown'}`\n\n"
        f"Evidence:\n{evidence_lines}\n\n"
        "Suggested checks:\n"
        + "\n".join(f"- `{check}`" for check in suggested_checks)
        + "\n\n"
        "Status: awaiting engineer review before broadcast."
    )
    return {
        "summary": summary,
        "confidence": "high" if len(evidence) >= 4 else "medium" if evidence else "low",
        "draft_message": draft,
        "steps": evidence,
    }
