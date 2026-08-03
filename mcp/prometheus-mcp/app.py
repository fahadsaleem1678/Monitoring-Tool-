import os
import time

import httpx
from fastapi import FastAPI, HTTPException

app = FastAPI()
PROMETHEUS_URL = os.getenv("PROMETHEUS_URL", "http://localhost:9090").rstrip("/")
MAX_RANGE_SECONDS = 60 * 60


@app.get("/healthz")
async def healthz():
    return {"status": "healthy", "service": "prometheus-mcp"}


@app.post("/tools/prometheus.query")
async def prometheus_query(payload: dict):
    query = str(payload.get("query", "")).strip()
    if not query:
        raise HTTPException(status_code=400, detail="query is required")
    body = await prometheus_get("/api/v1/query", {"query": query})
    result = body.get("data", {}).get("result", [])
    return {
        "tool": "prometheus.query",
        "query": query,
        "summary": f"Prometheus returned {len(result)} series",
        "data": body.get("data", {}),
    }


@app.post("/tools/prometheus.query_range")
async def prometheus_query_range(payload: dict):
    query = str(payload.get("query", "")).strip()
    if not query:
        raise HTTPException(status_code=400, detail="query is required")
    end = _number(payload.get("end"), time.time())
    start = _number(payload.get("start"), end - MAX_RANGE_SECONDS)
    step = max(_number(payload.get("step"), 60), 30)
    if end <= start:
        raise HTTPException(status_code=400, detail="end must be after start")
    if end - start > MAX_RANGE_SECONDS:
        start = end - MAX_RANGE_SECONDS
    body = await prometheus_get("/api/v1/query_range", {"query": query, "start": start, "end": end, "step": step})
    result = body.get("data", {}).get("result", [])
    return {
        "tool": "prometheus.query_range",
        "query": query,
        "summary": f"Prometheus returned {len(result)} range series over {int(end - start)} seconds",
        "data": body.get("data", {}),
    }


@app.post("/tools/prometheus.alerts")
async def prometheus_alerts(_: dict):
    body = await prometheus_get("/api/v1/alerts", {})
    alerts = body.get("data", {}).get("alerts", [])
    firing = [alert for alert in alerts if alert.get("state") == "firing"]
    return {
        "tool": "prometheus.alerts",
        "summary": f"Prometheus has {len(firing)} firing alert(s) out of {len(alerts)} active alert(s)",
        "data": {"alerts": alerts[:50]},
    }


@app.post("/tools/prometheus.rules")
async def prometheus_rules(payload: dict):
    query = str(payload.get("query", "")).strip()
    body = await prometheus_get("/api/v1/rules", {})
    groups = body.get("data", {}).get("groups", [])
    matches = []
    for group in groups:
        for rule in group.get("rules", []):
            rule_query = str(rule.get("query", ""))
            rule_name = str(rule.get("name", ""))
            if not query or query in rule_query or rule_query in query:
                matches.append(
                    {
                        "group": group.get("name", ""),
                        "name": rule_name,
                        "type": rule.get("type", ""),
                        "state": rule.get("state", ""),
                        "query": rule_query,
                        "labels": rule.get("labels", {}),
                    }
                )
    return {
        "tool": "prometheus.rules",
        "summary": f"Prometheus returned {len(matches)} relevant rule(s)",
        "data": {"rules": matches[:30]},
    }


@app.post("/tools/prometheus.targets")
async def prometheus_targets(_: dict):
    body = await prometheus_get("/api/v1/targets", {})
    active = body.get("data", {}).get("activeTargets", [])
    unhealthy = [target for target in active if str(target.get("health", "")).lower() != "up"]
    targets = [
        {
            "scrape_url": target.get("scrapeUrl", ""),
            "health": target.get("health", ""),
            "last_error": target.get("lastError", ""),
            "labels": target.get("labels", {}),
        }
        for target in active[:50]
    ]
    return {
        "tool": "prometheus.targets",
        "summary": f"Prometheus reports {len(unhealthy)} unhealthy target(s) out of {len(active)} active target(s)",
        "data": {"targets": targets, "unhealthy_count": len(unhealthy), "active_count": len(active)},
    }


async def prometheus_get(path: str, params: dict) -> dict:
    async with httpx.AsyncClient(timeout=10) as client:
        response = await client.get(f"{PROMETHEUS_URL}{path}", params=params)
        response.raise_for_status()
        return response.json()


def _number(value, fallback: float) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return fallback
