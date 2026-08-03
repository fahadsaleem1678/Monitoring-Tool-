import json
from typing import Any

import httpx
import yaml


class KubernetesMCPClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    async def pods(self, namespace: str = "") -> dict:
        return await self._post("kubernetes.pods", {"namespace": namespace})

    async def events(self, namespace: str = "") -> dict:
        return await self._post("kubernetes.events", {"namespace": namespace})

    async def deployments(self, namespace: str = "") -> dict:
        return await self._post("kubernetes.deployments", {"namespace": namespace})

    async def logs(self, namespace: str, pod: str, tail_lines: int = 80) -> dict:
        return await self._post("kubernetes.logs", {"namespace": namespace, "pod": pod, "tail_lines": tail_lines})

    async def _post(self, tool: str, payload: dict) -> dict:
        async with httpx.AsyncClient(timeout=15) as client:
            response = await client.post(f"{self.base_url}/tools/{tool}", json=payload)
            response.raise_for_status()
            return response.json()


class OfficialKubernetesMCPClient:
    def __init__(self, base_url: str, timeout_seconds: float = 5):
        self.base_url = base_url.rstrip("/")
        self.timeout_seconds = timeout_seconds

    async def pods(self, namespace: str = "") -> dict:
        if namespace:
            result = await self._tool("pods_list_in_namespace", {"namespace": namespace}, f"kubectl get pods -n {namespace}")
        else:
            result = await self._tool("pods_list", {}, "kubectl get pods -A")
        return self._pod_list_result(result)

    async def events(self, namespace: str = "") -> dict:
        return await self._resource_list("Event", "v1", namespace, "events")

    async def deployments(self, namespace: str = "") -> dict:
        return await self._resource_list("Deployment", "apps/v1", namespace, "deployments")

    async def logs(self, namespace: str, pod: str, tail_lines: int = 80) -> dict:
        result = await self._tool(
            "pods_log",
            {"namespace": namespace, "name": pod, "tail": tail_lines},
            f"kubectl logs {pod} -n {namespace} --tail={tail_lines}",
        )
        text = result["data"].pop("_text", result["data"].get("text_preview", ""))
        if not text.strip():
            result = await self._tool(
                "pods_log",
                {"namespace": namespace, "name": pod, "tail": tail_lines, "previous": True},
                f"kubectl logs {pod} -n {namespace} --tail={tail_lines} --previous",
            )
            text = result["data"].pop("_text", result["data"].get("text_preview", ""))
        lines = [line for line in text.splitlines() if line.strip()]
        result["summary"] = f"Kubernetes returned {len(lines)} log line(s) for {pod} via official MCP"
        result["data"] = {"namespace": namespace, "pod": pod, "preview": "\n".join(lines[-20:]), "source": "official"}
        return result

    async def tools_list(self) -> dict:
        body = await self._mcp_request("tools/list", {})
        if "error" in body:
            raise ValueError(str(body["error"]))
        tools = body.get("result", {}).get("tools", [])
        return {
            "tool": "official-kubernetes.tools_list",
            "summary": f"Official Kubernetes MCP exposed {len(tools)} tools",
            "data": {"tool_names": [tool.get("name", "") for tool in tools if isinstance(tool, dict)]},
        }

    async def _resource_list(self, kind: str, api_version: str, namespace: str, label: str) -> dict:
        arguments = {"apiVersion": api_version, "kind": kind}
        if namespace:
            arguments["namespace"] = namespace
        result = await self._tool(
            "resources_list",
            arguments,
            f"kubectl get {label} -n {namespace}" if namespace else f"kubectl get {label} -A",
        )
        parsed = _parse_yamlish_items(result["data"].pop("_text", result["data"].get("text_preview", "")))
        if kind == "Pod":
            return self._pod_list_result(result, parsed)
        elif kind == "Event":
            events = [_summarize_official_event(item) for item in parsed[-20:]]
            result["summary"] = f"Kubernetes returned {len(events)} event(s) via official MCP"
            result["data"] = {"events": events, "source": "official", "text_preview": result["data"].get("text_preview", "")[:1000]}
        elif kind == "Deployment":
            deployments = [_summarize_official_deployment(item) for item in parsed[:50]]
            unavailable = [item for item in deployments if int(item.get("unavailable_replicas") or 0) > 0]
            result["summary"] = f"Kubernetes returned {len(deployments)} deployment(s), {len(unavailable)} with unavailable replicas via official MCP"
            result["data"] = {"deployments": deployments, "source": "official", "text_preview": result["data"].get("text_preview", "")[:1000]}
        return result

    async def _tool(self, name: str, arguments: dict[str, Any], command: str) -> dict:
        body = await self._mcp_request("tools/call", {"name": name, "arguments": arguments})
        if "error" in body:
            raise ValueError(str(body["error"]))
        mcp_result = body.get("result", {})
        if mcp_result.get("isError"):
            raise ValueError(_content_text(mcp_result) or f"official Kubernetes MCP tool {name} failed")
        text = _content_text(mcp_result)
        structured = mcp_result.get("structuredContent")
        return {
            "tool": f"official-kubernetes.{name}",
            "query": command,
            "summary": f"Official Kubernetes MCP {name} completed",
            "data": {
                "text_preview": text[:2000],
                "_text": text,
                "structured_preview": _compact_value(structured),
                "source": "official",
            },
        }

    def _pod_list_result(self, result: dict, parsed: list[dict] | None = None) -> dict:
        if parsed is None:
            parsed = _parse_yamlish_items(result["data"].pop("_text", result["data"].get("text_preview", "")))
        pods = [_summarize_official_pod(item) for item in parsed[:50]]
        result["summary"] = f"Kubernetes returned {len(pods)} pod(s) via official MCP"
        result["data"] = {"pods": pods, "source": "official", "text_preview": result["data"].get("text_preview", "")[:1000]}
        return result

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
        raise last_error or RuntimeError("official Kubernetes MCP request failed")


class FailoverKubernetesMCPClient:
    def __init__(self, primary: OfficialKubernetesMCPClient, fallback: KubernetesMCPClient):
        self.primary = primary
        self.fallback = fallback

    async def pods(self, namespace: str = "") -> dict:
        return await self._with_fallback("pods", namespace)

    async def events(self, namespace: str = "") -> dict:
        return await self._with_fallback("events", namespace)

    async def deployments(self, namespace: str = "") -> dict:
        return await self._with_fallback("deployments", namespace)

    async def logs(self, namespace: str, pod: str, tail_lines: int = 80) -> dict:
        return await self._with_fallback("logs", namespace, pod, tail_lines)

    async def _with_fallback(self, method: str, *args) -> dict:
        try:
            result = await getattr(self.primary, method)(*args)
            if method == "pods" and not result.get("data", {}).get("pods"):
                raise ValueError("official Kubernetes MCP returned no parsed pods")
            return result
        except Exception as exc:
            result = await getattr(self.fallback, method)(*args)
            result["official_mcp_fallback"] = {"reason": str(exc), "fallback_tool": result.get("tool", "")}
            result["summary"] = f"{result.get('summary', 'Kubernetes fallback completed')} (official MCP fallback used)"
            return result


def kubernetes_client_from_env(mode: str, mvp_url: str, official_url: str = "", official_timeout_seconds: float = 5):
    if mode.strip().lower() == "official":
        return FailoverKubernetesMCPClient(
            OfficialKubernetesMCPClient(official_url or mvp_url, official_timeout_seconds),
            KubernetesMCPClient(mvp_url),
        )
    return KubernetesMCPClient(mvp_url)


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


def _compact_value(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: _compact_value(value[key]) for key in list(value.keys())[:20]}
    if isinstance(value, list):
        return [_compact_value(item) for item in value[:20]]
    if isinstance(value, str):
        return value[:1000]
    return value


def _parse_yamlish_items(text: str) -> list[dict]:
    parsed = _try_json(text)
    if parsed is None:
        parsed = _try_yaml(text)
    if isinstance(parsed, dict):
        items = parsed.get("items")
        return items if isinstance(items, list) else [parsed]
    if isinstance(parsed, list):
        return parsed
    return []


def _try_json(text: str) -> Any:
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


def _try_yaml(text: str) -> Any:
    try:
        return yaml.safe_load(text)
    except yaml.YAMLError:
        return None


def _summarize_official_pod(item: dict) -> dict:
    statuses = item.get("status", {}).get("containerStatuses", [])
    waiting_reasons = []
    restart_count = 0
    for status in statuses:
        restart_count += int(status.get("restartCount", 0))
        waiting = status.get("state", {}).get("waiting")
        if waiting and waiting.get("reason"):
            waiting_reasons.append(waiting["reason"])
    metadata = item.get("metadata", {})
    return {
        "namespace": metadata.get("namespace", ""),
        "name": metadata.get("name", ""),
        "phase": item.get("status", {}).get("phase", ""),
        "restart_count": restart_count,
        "waiting_reasons": waiting_reasons,
    }


def _summarize_official_event(item: dict) -> dict:
    metadata = item.get("metadata", {})
    return {
        "namespace": metadata.get("namespace", ""),
        "name": metadata.get("name", ""),
        "reason": item.get("reason", ""),
        "message": item.get("message", ""),
        "type": item.get("type", ""),
        "last_timestamp": item.get("lastTimestamp") or item.get("eventTime") or metadata.get("creationTimestamp", ""),
    }


def _summarize_official_deployment(item: dict) -> dict:
    metadata = item.get("metadata", {})
    status = item.get("status", {})
    return {
        "namespace": metadata.get("namespace", ""),
        "name": metadata.get("name", ""),
        "replicas": status.get("replicas", 0),
        "ready_replicas": status.get("readyReplicas", 0),
        "updated_replicas": status.get("updatedReplicas", 0),
        "unavailable_replicas": status.get("unavailableReplicas", 0),
        "conditions": [
            {
                "type": condition.get("type", ""),
                "status": condition.get("status", ""),
                "reason": condition.get("reason", ""),
                "message": condition.get("message", ""),
            }
            for condition in status.get("conditions", [])
        ],
    }
