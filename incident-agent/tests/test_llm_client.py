import json

from app.llm_client import build_incident_messages, parse_incident_draft


def test_build_incident_messages_contains_evidence_and_json_instruction():
    incident = {"id": "inc-1", "title": "CrashLoop", "severity": "critical"}
    evidence = [{"tool_name": "kubernetes-logs", "query_or_command": "kubectl logs api", "result_summary": "panic"}]

    messages = build_incident_messages(incident, evidence)

    assert messages[0]["role"] == "system"
    assert "Return valid JSON only" in messages[0]["content"]
    user_payload = json.loads(messages[1]["content"])
    assert user_payload["incident"]["title"] == "CrashLoop"
    assert "kubernetes-logs" in user_payload["evidence"]
    assert "slack_message" in user_payload["required_json_shape"]


def test_parse_incident_draft_normalizes_fields():
    content = """
    ```json
    {
      "summary": "API pod is restarting.",
      "probable_cause": "CrashLoopBackOff after config change.",
      "confidence": "HIGH",
      "evidence_summary": ["restarts increased", "logs show config error"],
      "suggested_next_checks": "kubectl describe pod api",
      "slack_message": "API is unstable and needs review."
    }
    ```
    """

    parsed = parse_incident_draft(content)

    assert parsed["summary"] == "API pod is restarting."
    assert parsed["probable_cause"] == "CrashLoopBackOff after config change."
    assert parsed["confidence"] == "high"
    assert parsed["evidence_summary"] == ["restarts increased", "logs show config error"]
    assert parsed["suggested_next_checks"] == ["kubectl describe pod api"]
    assert parsed["slack_message"] == "API is unstable and needs review."
