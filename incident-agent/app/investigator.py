def build_draft(incident: dict, evidence: list[dict]) -> dict:
    title = incident.get("title", "Monitoring incident")
    severity = incident.get("severity", "warning")
    summary = f"{title} requires review. The agent collected {len(evidence)} evidence item(s)."
    evidence_lines = "\n".join(
        f"- {item.get('tool_name', 'tool')}: `{item.get('query_or_command', '')}` -> {item.get('result_summary', '')}"
        for item in evidence
    )
    draft = (
        f":rotating_light: Monitoring Tool incident draft: {title}\n"
        f"Severity: {severity}\n"
        f"Summary: {summary}\n\n"
        f"Evidence:\n{evidence_lines}\n\n"
        "Status: awaiting engineer review before broadcast."
    )
    return {
        "summary": summary,
        "confidence": "medium" if evidence else "low",
        "draft_message": draft,
        "steps": evidence,
    }
