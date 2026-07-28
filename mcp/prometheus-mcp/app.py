import os

import httpx
from fastapi import FastAPI, HTTPException

app = FastAPI()
PROMETHEUS_URL = os.getenv("PROMETHEUS_URL", "http://localhost:9090").rstrip("/")


@app.get("/healthz")
async def healthz():
    return {"status": "healthy", "service": "prometheus-mcp"}


@app.post("/tools/prometheus.query")
async def prometheus_query(payload: dict):
    query = str(payload.get("query", "")).strip()
    if not query:
        raise HTTPException(status_code=400, detail="query is required")
    async with httpx.AsyncClient(timeout=10) as client:
        response = await client.get(f"{PROMETHEUS_URL}/api/v1/query", params={"query": query})
        response.raise_for_status()
        body = response.json()
    result = body.get("data", {}).get("result", [])
    return {
        "tool": "prometheus.query",
        "query": query,
        "summary": f"Prometheus returned {len(result)} series",
        "data": body.get("data", {}),
    }
