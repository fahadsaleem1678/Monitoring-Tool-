import os

import httpx
from fastapi import FastAPI

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
            "pods": [
                {
                    "namespace": item.get("metadata", {}).get("namespace", ""),
                    "name": item.get("metadata", {}).get("name", ""),
                    "phase": item.get("status", {}).get("phase", ""),
                }
                for item in items[:50]
            ]
        },
    }


async def kubernetes_get(path: str) -> dict:
    with open(TOKEN_PATH, encoding="utf-8") as token_file:
        token = token_file.read().strip()
    verify = CA_PATH if os.path.exists(CA_PATH) else True
    async with httpx.AsyncClient(timeout=10, verify=verify) as client:
        response = await client.get(
            f"https://{SERVICE_HOST}:{SERVICE_PORT}{path}",
            headers={"Authorization": f"Bearer {token}"},
        )
        response.raise_for_status()
        return response.json()
