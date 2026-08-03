import json
import os
import re
from typing import Any

import httpx


VALID_CONFIDENCE = {"low", "medium", "high"}


class LLMClient:
    def __init__(self, provider: str, model: str, base_url: str = "", api_key: str = ""):
        self.provider = provider.strip().lower()
        self.model = model.strip()
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key.strip()

    @classmethod
    def from_env(cls) -> "LLMClient":
        provider = os.getenv("LLM_PROVIDER", "ollama").strip().lower()
        if provider == "openai":
            return cls(
                provider="openai",
                model=os.getenv("OPENAI_MODEL", "gpt-4.1-mini"),
                base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
                api_key=os.getenv("OPENAI_API_KEY", ""),
            )
        return cls(
            provider="ollama",
            model=os.getenv("OLLAMA_MODEL", "qwen2.5:7b"),
            base_url=os.getenv("OLLAMA_URL", "http://host.docker.internal:11434"),
        )

    async def generate_incident_draft(self, incident: dict, evidence: list[dict]) -> dict:
        messages = build_incident_messages(incident, evidence)
        if self.provider == "openai":
            content = await self._openai_chat(messages)
        elif self.provider == "ollama":
            content = await self._ollama_chat(messages)
        else:
            raise ValueError(f"unsupported LLM_PROVIDER {self.provider!r}")
        parsed = parse_incident_draft(content)
        parsed["provider"] = self.provider
        parsed["model"] = self.model
        return parsed

    async def _ollama_chat(self, messages: list[dict[str, str]]) -> str:
        async with httpx.AsyncClient(timeout=60) as client:
            response = await client.post(
                f"{self.base_url}/api/chat",
                json={
                    "model": self.model,
                    "messages": messages,
                    "stream": False,
                    "format": "json",
                    "options": {"temperature": 0.2},
                },
            )
            response.raise_for_status()
            body = response.json()
        return str(body.get("message", {}).get("content", ""))

    async def _openai_chat(self, messages: list[dict[str, str]]) -> str:
        if not self.api_key:
            raise ValueError("OPENAI_API_KEY is required when LLM_PROVIDER=openai")
        async with httpx.AsyncClient(timeout=60) as client:
            response = await client.post(
                f"{self.base_url}/chat/completions",
                headers={"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"},
                json={
                    "model": self.model,
                    "messages": messages,
                    "temperature": 0.2,
                    "response_format": {"type": "json_object"},
                },
            )
            response.raise_for_status()
            body = response.json()
        return str(body.get("choices", [{}])[0].get("message", {}).get("content", ""))


def build_incident_messages(incident: dict, evidence: list[dict], max_evidence_chars: int = 12000) -> list[dict[str, str]]:
    compact_evidence = [
        {
            "step_type": item.get("step_type", ""),
            "tool_name": item.get("tool_name", ""),
            "query_or_command": item.get("query_or_command", ""),
            "result_summary": item.get("result_summary", ""),
            "raw_result_json": item.get("raw_result_json", {}),
        }
        for item in evidence
    ]
    evidence_json = json.dumps(compact_evidence, ensure_ascii=True, default=str)
    if len(evidence_json) > max_evidence_chars:
        evidence_json = evidence_json[:max_evidence_chars] + "...[truncated]"

    system = (
        "You are a cautious Kubernetes and Prometheus incident assistant. "
        "You cannot mutate infrastructure, approve Slack messages, or call tools. "
        "Use only the evidence provided. If the evidence is thin, say so. "
        "Return valid JSON only."
    )
    user = {
        "incident": {
            "id": incident.get("id", ""),
            "title": incident.get("title", ""),
            "severity": incident.get("severity", ""),
            "summary": incident.get("summary", ""),
            "draft_message": incident.get("draft_message", ""),
        },
        "required_json_shape": {
            "summary": "one concise incident summary",
            "probable_cause": "most likely cause, or unknown if evidence is insufficient",
            "confidence": "low, medium, or high",
            "evidence_summary": ["short bullet grounded in evidence"],
            "suggested_next_checks": ["safe read-only next check"],
            "slack_message": "human-reviewable Slack incident draft",
        },
        "evidence": evidence_json,
    }
    return [{"role": "system", "content": system}, {"role": "user", "content": json.dumps(user, ensure_ascii=True)}]


def parse_incident_draft(content: str) -> dict[str, Any]:
    body = _extract_json_object(content)
    parsed = json.loads(body)
    confidence = str(parsed.get("confidence", "low")).strip().lower()
    if confidence not in VALID_CONFIDENCE:
        confidence = "low"
    evidence_summary = _string_list(parsed.get("evidence_summary"))
    suggested_next_checks = _string_list(parsed.get("suggested_next_checks"))
    return {
        "summary": _clean_text(parsed.get("summary")) or "LLM did not provide a summary.",
        "probable_cause": _clean_text(parsed.get("probable_cause")) or "Unknown from available evidence.",
        "confidence": confidence,
        "evidence_summary": evidence_summary,
        "suggested_next_checks": suggested_next_checks,
        "slack_message": _clean_text(parsed.get("slack_message")) or "Incident draft unavailable.",
    }


def _extract_json_object(content: str) -> str:
    stripped = content.strip()
    if stripped.startswith("{") and stripped.endswith("}"):
        return stripped
    fenced = re.search(r"```(?:json)?\s*(\{.*?\})\s*```", stripped, re.DOTALL)
    if fenced:
        return fenced.group(1)
    first = stripped.find("{")
    last = stripped.rfind("}")
    if first >= 0 and last > first:
        return stripped[first : last + 1]
    raise ValueError("LLM response did not contain a JSON object")


def _clean_text(value: Any) -> str:
    return str(value or "").strip()


def _string_list(value: Any) -> list[str]:
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    text = str(value or "").strip()
    return [text] if text else []
