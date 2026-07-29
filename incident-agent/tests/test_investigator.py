from app.investigator import build_draft, build_related_promql_queries, choose_pod_for_logs, extract_alert_query, extract_label_value


def test_build_draft_includes_query_and_evidence():
    incident = {
        "title": "Targets down",
        "severity": "critical",
        "summary": 'up target is down\nQuery: `sum(up == 0)`',
    }
    evidence = [
        {"tool_name": "prometheus", "query_or_command": "sum(up == 0)", "result_summary": "1 target is down"},
        {"tool_name": "kubernetes", "query_or_command": "kubectl get pods -A", "result_summary": "all monitoring pods running"},
    ]

    result = build_draft(incident, evidence)

    assert result["confidence"] == "medium"
    assert "Targets down" in result["summary"]
    assert "sum(up == 0)" in result["draft_message"]
    assert "Alert query" in result["draft_message"]
    assert result["steps"] == evidence


def test_extract_alert_query_from_incident_summary():
    incident = {"summary": 'Severity: critical\nQuery: `sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`'}

    assert (
        extract_alert_query(incident)
        == 'sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})'
    )


def test_extract_label_value_ignores_regex_values():
    query = 'sum(kube_pod_info{namespace="monitoring-tool", pod=~"api-.*"})'

    assert extract_label_value(query, "namespace") == "monitoring-tool"
    assert extract_label_value(query, "pod") == ""


def test_build_related_queries_for_crashloop_namespace():
    query = 'sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})'

    related = build_related_promql_queries(query)

    assert 'kube_pod_container_status_waiting_reason{namespace="monitoring-tool"}' in related[0]
    assert 'increase(kube_pod_container_status_restarts_total{namespace="monitoring-tool"}[10m])' in related[1]


def test_choose_pod_for_logs_prefers_waiting_reason():
    pods_result = {
        "data": {
            "pods": [
                {"namespace": "monitoring-tool", "name": "healthy", "phase": "Running", "restart_count": 0, "waiting_reasons": []},
                {
                    "namespace": "monitoring-tool",
                    "name": "broken-api-123",
                    "phase": "Running",
                    "restart_count": 5,
                    "waiting_reasons": ["CrashLoopBackOff"],
                },
            ]
        }
    }

    assert choose_pod_for_logs(pods_result) == ("monitoring-tool", "broken-api-123")
