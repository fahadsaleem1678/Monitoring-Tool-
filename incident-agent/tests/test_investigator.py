from app.investigator import build_draft


def test_build_draft_includes_query_and_evidence():
    incident = {
        "title": "Targets down",
        "severity": "critical",
        "summary": "up target is down",
    }
    evidence = [
        {"tool_name": "prometheus", "query_or_command": "sum(up == 0)", "result_summary": "1 target is down"},
        {"tool_name": "kubernetes", "query_or_command": "kubectl get pods -A", "result_summary": "all monitoring pods running"},
    ]

    result = build_draft(incident, evidence)

    assert result["confidence"] == "medium"
    assert "Targets down" in result["summary"]
    assert "sum(up == 0)" in result["draft_message"]
    assert result["steps"] == evidence
