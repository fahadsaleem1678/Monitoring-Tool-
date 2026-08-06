import json

from app.llm_client import LLMClient, build_incident_messages, parse_incident_draft


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


def test_build_incident_messages_compacts_raw_evidence():
    incident = {"id": "inc-1", "title": "CrashLoop", "severity": "critical"}
    evidence = [
        {
            "tool_name": "official-kubernetes.pods_list_in_namespace",
            "query_or_command": "kubectl get pods",
            "result_summary": "pods checked",
            "raw_result_json": {
                "data": {
                    "pods": [{"name": "broken-api", "waiting_reasons": ["CrashLoopBackOff"]}],
                    "text_preview": "x" * 10000,
                }
            },
        }
    ]

    messages = build_incident_messages(incident, evidence, max_evidence_chars=4000)
    user_payload = json.loads(messages[1]["content"])

    assert "broken-api" in user_payload["evidence"]
    assert "x" * 1000 not in user_payload["evidence"]


def test_llm_client_from_env_uses_ollama_timeout(monkeypatch):
    monkeypatch.setenv("LLM_PROVIDER", "ollama")
    monkeypatch.setenv("OLLAMA_MODEL", "llama3.2:1b")
    monkeypatch.setenv("OLLAMA_TIMEOUT_SECONDS", "240")
    monkeypatch.setenv("MAX_LLM_EVIDENCE_CHARS", "5000")

    client = LLMClient.from_env()

    assert client.model == "llama3.2:1b"
    assert client.timeout_seconds == 240
    assert client.max_evidence_chars == 5000


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
