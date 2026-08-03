import os
from urllib.parse import urlencode

import httpx
from fastapi import FastAPI, HTTPException

app = FastAPI()
SERVICE_HOST = os.getenv("KUBERNETES_SERVICE_HOST", "kubernetes.default.svc")
SERVICE_PORT = os.getenv("KUBERNETES_SERVICE_PORT", "443")
TOKEN_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/token"
CA_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"


@app.get("/healthz")
async def healthz():
    return {"status": "healthy", "service": "kubernetes-mcp"}


@app.post("/tools/kubernetes.pods")
async def kubernetes_pods(payload: dict):
    namespace = str(payload.get("namespace", "")).strip()
    path = f"/api/v1/namespaces/{namespace}/pods" if namespace else "/api/v1/pods"
    body = await kubernetes_get(path)
    items = body.get("items", [])
    return {
        "tool": "kubernetes.pods",
        "summary": f"Kubernetes returned {len(items)} pod(s)",
        "data": {
            "pods": [summarize_pod(item) for item in items[:50]]
        },
    }


@app.post("/tools/kubernetes.events")
async def kubernetes_events(payload: dict):
    namespace = str(payload.get("namespace", "")).strip()
    path = f"/api/v1/namespaces/{namespace}/events" if namespace else "/api/v1/events"
    body = await kubernetes_get(path)
    items = body.get("items", [])
    events = [
        {
            "namespace": item.get("metadata", {}).get("namespace", ""),
            "name": item.get("metadata", {}).get("name", ""),
            "reason": item.get("reason", ""),
            "message": item.get("message", ""),
            "type": item.get("type", ""),
            "last_timestamp": item.get("lastTimestamp") or item.get("eventTime") or item.get("metadata", {}).get("creationTimestamp", ""),
        }
        for item in items[-20:]
    ]
    return {
        "tool": "kubernetes.events",
        "summary": f"Kubernetes returned {len(items)} event(s)",
        "data": {"events": events},
    }


@app.post("/tools/kubernetes.deployments")
async def kubernetes_deployments(payload: dict):
    namespace = str(payload.get("namespace", "")).strip()
    path = f"/apis/apps/v1/namespaces/{namespace}/deployments" if namespace else "/apis/apps/v1/deployments"
    body = await kubernetes_get(path)
    items = body.get("items", [])
    deployments = [
        {
            "namespace": item.get("metadata", {}).get("namespace", ""),
            "name": item.get("metadata", {}).get("name", ""),
            "replicas": item.get("status", {}).get("replicas", 0),
            "ready_replicas": item.get("status", {}).get("readyReplicas", 0),
            "updated_replicas": item.get("status", {}).get("updatedReplicas", 0),
            "unavailable_replicas": item.get("status", {}).get("unavailableReplicas", 0),
            "conditions": [
                {
                    "type": condition.get("type", ""),
                    "status": condition.get("status", ""),
                    "reason": condition.get("reason", ""),
                    "message": condition.get("message", ""),
                }
                for condition in item.get("status", {}).get("conditions", [])
            ],
        }
        for item in items[:50]
    ]
    unavailable = [item for item in deployments if int(item.get("unavailable_replicas") or 0) > 0]
    return {
        "tool": "kubernetes.deployments",
        "summary": f"Kubernetes returned {len(items)} deployment(s), {len(unavailable)} with unavailable replicas",
        "data": {"deployments": deployments},
    }


@app.post("/tools/kubernetes.logs")
async def kubernetes_logs(payload: dict):
    namespace = str(payload.get("namespace", "")).strip()
    pod = str(payload.get("pod", "")).strip()
    if not namespace or not pod:
        raise HTTPException(status_code=400, detail="namespace and pod are required")
    tail_lines = int(payload.get("tail_lines", 80))
    if tail_lines <= 0 or tail_lines > 200:
        tail_lines = 80
    params = urlencode({"tailLines": tail_lines})
    path = f"/api/v1/namespaces/{namespace}/pods/{pod}/log?{params}"
    try:
        text = await kubernetes_get_text(path)
    except httpx.HTTPStatusError:
        previous_params = urlencode({"tailLines": tail_lines, "previous": "true"})
        text = await kubernetes_get_text(f"/api/v1/namespaces/{namespace}/pods/{pod}/log?{previous_params}")
    lines = [line for line in text.splitlines() if line.strip()]
    preview = "\n".join(lines[-20:])
    return {
        "tool": "kubernetes.logs",
        "summary": f"Kubernetes returned {len(lines)} log line(s) for {pod}",
        "data": {"namespace": namespace, "pod": pod, "preview": preview},
    }


def summarize_pod(item: dict) -> dict:
    statuses = item.get("status", {}).get("containerStatuses", [])
    waiting_reasons = []
    restart_count = 0
    for status in statuses:
        restart_count += int(status.get("restartCount", 0))
        waiting = status.get("state", {}).get("waiting")
        if waiting:
            reason = waiting.get("reason", "")
            if reason:
                waiting_reasons.append(reason)
    return {
        "namespace": item.get("metadata", {}).get("namespace", ""),
        "name": item.get("metadata", {}).get("name", ""),
        "phase": item.get("status", {}).get("phase", ""),
        "restart_count": restart_count,
        "waiting_reasons": waiting_reasons,
    }


async def kubernetes_get(path: str) -> dict:
    response = await kubernetes_request(path)
    return response.json()


async def kubernetes_get_text(path: str) -> str:
    response = await kubernetes_request(path)
    return response.text


async def kubernetes_request(path: str) -> httpx.Response:
    with open(TOKEN_PATH, encoding="utf-8") as token_file:
        token = token_file.read().strip()
    verify = CA_PATH if os.path.exists(CA_PATH) else True
    async with httpx.AsyncClient(timeout=10, verify=verify) as client:
        response = await client.get(
            f"https://{SERVICE_HOST}:{SERVICE_PORT}{path}",
            headers={"Authorization": f"Bearer {token}"},
        )
        response.raise_for_status()
        return response
