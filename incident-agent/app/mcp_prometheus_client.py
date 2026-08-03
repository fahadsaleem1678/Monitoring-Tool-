import httpx
import json
import re
from typing import Any


class PrometheusMCPClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    async def instant_query(self, query: str) -> dict:
        return await self._post("prometheus.query", {"query": query})

    async def range_query(self, query: str, start: int, end: int, step: int) -> dict:
        return await self._post("prometheus.query_range", {"query": query, "start": start, "end": end, "step": step})

    async def active_alerts(self) -> dict:
        return await self._post("prometheus.alerts", {})

    async def rules(self, query: str = "") -> dict:
        return await self._post("prometheus.rules", {"query": query})

    async def targets(self) -> dict:
        return await self._post("prometheus.targets", {})

    async def _post(self, tool: str, payload: dict) -> dict:
        async with httpx.AsyncClient(timeout=15) as client:
            response = await client.post(f"{self.base_url}/tools/{tool}", json=payload)
            response.raise_for_status()
            return response.json()


class OfficialPrometheusMCPClient:
    def __init__(self, base_url: str, timeout_seconds: float = 5):
        self.base_url = base_url.rstrip("/")
        self.timeout_seconds = timeout_seconds

    async def instant_query(self, query: str) -> dict:
        return await self._tool("execute_query", {"query": query}, f"execute_query: {query}")

    async def range_query(self, query: str, start: int, end: int, step: int) -> dict:
        return await self._tool(
            "execute_range_query",
            {"query": query, "start": str(start), "end": str(end), "step": f"{step}s"},
            f"execute_range_query: {query}",
        )

    async def active_alerts(self) -> dict:
        return await self._tool("execute_query", {"query": 'ALERTS{alertstate="firing"}'}, "execute_query: firing alerts")

    async def rules(self, query: str = "") -> dict:
        metric = _metric_name(query) or "up"
        return await self._tool("get_metric_metadata", {"metric": metric}, f"get_metric_metadata: {metric}")

    async def targets(self) -> dict:
        return await self._tool("get_targets", {}, "get_targets")

    async def tools_list(self) -> dict:
        body = await self._mcp_request("tools/list", {})
        if "error" in body:
            raise ValueError(str(body["error"]))
        tools = body.get("result", {}).get("tools", [])
        return {
            "tool": "official-prometheus.tools_list",
            "summary": f"Official Prometheus MCP exposed {len(tools)} tools",
            "data": {"mcp_result": body.get("result", {})},
        }

    async def _tool(self, name: str, arguments: dict[str, Any], command: str) -> dict:
        body = await self._mcp_request("tools/call", {"name": name, "arguments": arguments})
        if "error" in body:
            raise ValueError(str(body["error"]))
        result = body.get("result", {})
        if result.get("isError"):
            raise ValueError(_content_text(result) or f"official Prometheus MCP tool {name} failed")
        text = _content_text(result)
        compact_data = _compact_official_result(result, text)
        return {
            "tool": f"official-prometheus.{name}",
            "query": command,
            "summary": _summary(name, text, result, compact_data),
            "data": compact_data,
        }

    async def _mcp_request(self, method: str, params: dict[str, Any]) -> dict:
        payload = {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
        last_error: Exception | None = None
        async with httpx.AsyncClient(timeout=self.timeout_seconds, follow_redirects=True) as client:
            for path in ("/mcp", "/mcp/"):
                try:
                    response = await client.post(
                        f"{self.base_url}{path}",
                        headers={"Accept": "application/json, text/event-stream", "Content-Type": "application/json"},
                        json=payload,
                    )
                    response.raise_for_status()
                    return _decode_mcp_response(response)
                except Exception as exc:
                    last_error = exc
        raise last_error or RuntimeError("official Prometheus MCP request failed")


class FailoverPrometheusMCPClient:
    def __init__(self, primary: OfficialPrometheusMCPClient, fallback: PrometheusMCPClient):
        self.primary = primary
        self.fallback = fallback

    async def instant_query(self, query: str) -> dict:
        return await self._with_fallback("instant_query", query)

    async def range_query(self, query: str, start: int, end: int, step: int) -> dict:
        return await self._with_fallback("range_query", query, start, end, step)

    async def active_alerts(self) -> dict:
        return await self._with_fallback("active_alerts")

    async def rules(self, query: str = "") -> dict:
        return await self._with_fallback("rules", query)

    async def targets(self) -> dict:
        return await self._with_fallback("targets")

    async def _with_fallback(self, method: str, *args) -> dict:
        try:
            return await getattr(self.primary, method)(*args)
        except Exception as exc:
            result = await getattr(self.fallback, method)(*args)
            result["official_mcp_fallback"] = {
                "reason": str(exc),
                "fallback_tool": result.get("tool", ""),
            }
            result["summary"] = f"{result.get('summary', 'Prometheus fallback completed')} (official MCP fallback used)"
            return result


def prometheus_client_from_env(mode: str, mvp_url: str, official_url: str = "", official_timeout_seconds: float = 5):
    if mode.strip().lower() == "official":
        return FailoverPrometheusMCPClient(
            OfficialPrometheusMCPClient(official_url or mvp_url, official_timeout_seconds),
            PrometheusMCPClient(mvp_url),
        )
    return PrometheusMCPClient(mvp_url)


def _decode_mcp_response(response: httpx.Response) -> dict:
    content_type = response.headers.get("content-type", "")
    if "text/event-stream" not in content_type:
        return response.json()
    for line in response.text.splitlines():
        if line.startswith("data:"):
            data = line.removeprefix("data:").strip()
            if data and data != "[DONE]":
                return json.loads(data)
    raise ValueError("MCP response did not include JSON data")


def _content_text(result: dict) -> str:
    chunks = []
    for item in result.get("content", []):
        if isinstance(item, dict) and item.get("type") == "text":
            chunks.append(str(item.get("text", "")))
    return "\n".join(chunk for chunk in chunks if chunk)


def _summary(tool_name: str, text: str, result: dict, compact_data: dict | None = None) -> str:
    if result.get("isError"):
        return f"Official Prometheus MCP {tool_name} returned an error"
    if compact_data:
        result_type = compact_data.get("resultType")
        if "series_count" in compact_data:
            return f"Prometheus returned {compact_data['series_count']} series via official MCP"
        if "target_count" in compact_data:
            return f"Prometheus returned {compact_data['target_count']} scrape targets via official MCP"
        if "metadata_count" in compact_data:
            return f"Prometheus returned {compact_data['metadata_count']} metadata entries via official MCP"
    if not text:
        return f"Official Prometheus MCP {tool_name} completed"
    compact = " ".join(text.split())
    return compact[:220]


def _compact_official_result(result: dict, text: str) -> dict:
    structured = result.get("structuredContent")
    if structured is None:
        structured = _parse_json_text(text)

    compact = _compact_structured_payload(structured)
    if text:
        compact["text_preview"] = text[:1000]
    compact["isError"] = bool(result.get("isError"))
    return compact


def _compact_structured_payload(value: Any) -> dict:
    if isinstance(value, dict) and isinstance(value.get("result"), list) and "resultType" in value:
        rows = value.get("result", [])
        return {
            "resultType": value.get("resultType"),
            "series_count": len(rows),
            "result": [_compact_series(item) for item in rows[:10]],
            "truncated": len(rows) > 10,
        }
    if isinstance(value, dict) and "active" in value:
        active = value.get("active", [])
        dropped = value.get("dropped", [])
        active_list = active if isinstance(active, list) else []
        dropped_list = dropped if isinstance(dropped, list) else []
        return {
            "target_count": len(active_list) + len(dropped_list),
            "active_count": len(active_list),
            "dropped_count": len(dropped_list),
            "active": [_compact_target(item) for item in active_list[:10]],
            "dropped": [_compact_target(item) for item in dropped_list[:10]],
            "truncated": len(active_list) > 10 or len(dropped_list) > 10,
        }
    if isinstance(value, dict) and "result" in value:
        metadata = value.get("result")
        if isinstance(metadata, list):
            return {"metadata_count": len(metadata), "result": metadata[:20], "truncated": len(metadata) > 20}
        if isinstance(metadata, dict):
            return {"metadata_count": len(metadata), "result": _first_items(metadata, 20), "truncated": len(metadata) > 20}
    if isinstance(value, dict):
        return _first_items(value, 30)
    if value is None:
        return {}
    return {"value": str(value)[:1000]}


def _compact_series(item: Any) -> Any:
    if not isinstance(item, dict):
        return item
    compact = {"metric": item.get("metric", {})}
    value = item.get("value")
    values = item.get("values")
    if value is not None:
        compact["value"] = value
    if isinstance(values, list):
        compact["values_count"] = len(values)
        compact["values_tail"] = values[-5:]
    return compact


def _compact_target(item: Any) -> Any:
    if not isinstance(item, dict):
        return item
    labels = item.get("labels") if isinstance(item.get("labels"), dict) else {}
    discovered_labels = item.get("discoveredLabels") if isinstance(item.get("discoveredLabels"), dict) else {}
    return {
        "health": item.get("health"),
        "scrapeUrl": item.get("scrapeUrl"),
        "lastError": item.get("lastError"),
        "labels": _first_items(labels, 12),
        "discoveredLabels": _first_items(discovered_labels, 12),
    }


def _first_items(value: dict, limit: int) -> dict:
    return {key: value[key] for key in list(value.keys())[:limit]}


def _parse_json_text(text: str) -> Any:
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


def _metric_name(query: str) -> str:
    functions = {"avg", "count", "increase", "max", "min", "rate", "sum"}
    for match in re.finditer(r"([a-zA-Z_:][a-zA-Z0-9_:]*)", query):
        value = match.group(1)
        if value not in functions and not value.startswith("__"):
            return value
    return ""
