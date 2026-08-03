import asyncio
import logging
import os

from fastapi import FastAPI
from prometheus_client import Counter, Histogram, generate_latest
from starlette.responses import JSONResponse, Response

from app.backend_client import BackendClient
from app.investigation_runner import InvestigationRunner, SafetyLimits
from app.llm_client import LLMClient
from app.mcp_kubernetes_client import OfficialKubernetesMCPClient, kubernetes_client_from_env
from app.mcp_prometheus_client import OfficialPrometheusMCPClient, prometheus_client_from_env


app = FastAPI()
logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("incident-agent")

investigations_started = Counter("incident_agent_investigations_started_total", "Investigations started")
investigations_completed = Counter("incident_agent_investigations_completed_total", "Investigations completed")
investigations_failed = Counter("incident_agent_investigations_failed_total", "Investigations failed")
investigation_duration = Histogram("incident_agent_investigation_duration_seconds", "Investigation duration")


@app.get("/healthz")
async def healthz():
    return {"status": "healthy", "service": "incident-agent"}


@app.get("/metrics")
async def metrics():
    return Response(generate_latest(), media_type="text/plain; version=0.0.4")


@app.get("/debug/prometheus-mcp")
async def debug_prometheus_mcp():
    client = _prometheus_client()
    payload = _prometheus_config_snapshot()
    try:
        payload["query_result"] = await client.instant_query("up")
        payload["ok"] = True
        return payload
    except Exception as exc:
        payload["ok"] = False
        payload["error"] = str(exc)
        logger.exception("Prometheus MCP debug query failed")
        return JSONResponse(payload, status_code=502)


@app.get("/debug/prometheus-mcp-official")
async def debug_prometheus_mcp_official():
    payload = _prometheus_config_snapshot()
    official = OfficialPrometheusMCPClient(
        os.getenv("OFFICIAL_PROMETHEUS_MCP_URL", os.environ["PROMETHEUS_MCP_URL"]),
        _official_timeout_seconds(),
    )
    try:
        payload["tools"] = await official.tools_list()
        payload["query_result"] = await official.instant_query("up")
        payload["ok"] = True
        return payload
    except Exception as exc:
        payload["ok"] = False
        payload["error"] = str(exc)
        logger.exception("Official Prometheus MCP debug check failed")
        return JSONResponse(payload, status_code=502)


@app.get("/debug/kubernetes-mcp")
async def debug_kubernetes_mcp():
    client = _kubernetes_client()
    payload = _kubernetes_config_snapshot()
    try:
        payload["pods_result"] = await client.pods(os.getenv("DEBUG_KUBERNETES_NAMESPACE", "monitoring-tool"))
        payload["ok"] = True
        return payload
    except Exception as exc:
        payload["ok"] = False
        payload["error"] = str(exc)
        logger.exception("Kubernetes MCP debug query failed")
        return JSONResponse(payload, status_code=502)


@app.get("/debug/kubernetes-mcp-official")
async def debug_kubernetes_mcp_official():
    payload = _kubernetes_config_snapshot()
    official = OfficialKubernetesMCPClient(
        os.getenv("OFFICIAL_KUBERNETES_MCP_URL", os.environ["KUBERNETES_MCP_URL"]),
        _official_kubernetes_timeout_seconds(),
    )
    try:
        payload["tools"] = await official.tools_list()
        payload["pods_result"] = await official.pods(os.getenv("DEBUG_KUBERNETES_NAMESPACE", "monitoring-tool"))
        payload["ok"] = True
        return payload
    except Exception as exc:
        payload["ok"] = False
        payload["error"] = str(exc)
        logger.exception("Official Kubernetes MCP debug check failed")
        return JSONResponse(payload, status_code=502)


async def investigate_once():
    backend = BackendClient(os.environ["BACKEND_URL"], os.getenv("AGENT_SERVICE_TOKEN", "dev-agent-token"))
    prometheus = _prometheus_client()
    kubernetes = _kubernetes_client()
    runner = InvestigationRunner(backend, prometheus, kubernetes, LLMClient.from_env(), SafetyLimits.from_env())
    with investigation_duration.time():
        try:
            investigated = await runner.investigate_once(investigations_started.inc)
            if investigated:
                investigations_completed.inc()
        except Exception:
            investigations_failed.inc()
            logger.exception("Incident investigation failed")
            raise


async def loop_forever():
    interval = int(os.getenv("AGENT_POLL_INTERVAL_SECONDS", "15"))
    while True:
        try:
            await investigate_once()
        except Exception:
            logger.exception("Incident investigation loop failed")
        await asyncio.sleep(interval)


@app.on_event("startup")
async def startup():
    logger.info(
        "Starting incident agent with Prometheus config %s and Kubernetes config %s",
        _prometheus_config_snapshot(),
        _kubernetes_config_snapshot(),
    )
    asyncio.create_task(loop_forever())


def _official_timeout_seconds() -> float:
    return float(os.getenv("OFFICIAL_PROMETHEUS_MCP_TIMEOUT_SECONDS", "5"))


def _official_kubernetes_timeout_seconds() -> float:
    return float(os.getenv("OFFICIAL_KUBERNETES_MCP_TIMEOUT_SECONDS", "5"))


def _prometheus_client():
    return prometheus_client_from_env(
        os.getenv("PROMETHEUS_MCP_MODE", "mvp"),
        os.environ["PROMETHEUS_MCP_URL"],
        os.getenv("OFFICIAL_PROMETHEUS_MCP_URL", ""),
        _official_timeout_seconds(),
    )


def _kubernetes_client():
    return kubernetes_client_from_env(
        os.getenv("KUBERNETES_MCP_MODE", "mvp"),
        os.environ["KUBERNETES_MCP_URL"],
        os.getenv("OFFICIAL_KUBERNETES_MCP_URL", ""),
        _official_kubernetes_timeout_seconds(),
    )


def _prometheus_config_snapshot() -> dict:
    return {
        "mode": os.getenv("PROMETHEUS_MCP_MODE", "mvp"),
        "mvp_url": os.getenv("PROMETHEUS_MCP_URL", ""),
        "official_url": os.getenv("OFFICIAL_PROMETHEUS_MCP_URL", ""),
        "official_timeout_seconds": _official_timeout_seconds(),
    }


def _kubernetes_config_snapshot() -> dict:
    return {
        "mode": os.getenv("KUBERNETES_MCP_MODE", "mvp"),
        "mvp_url": os.getenv("KUBERNETES_MCP_URL", ""),
        "official_url": os.getenv("OFFICIAL_KUBERNETES_MCP_URL", ""),
        "official_timeout_seconds": _official_kubernetes_timeout_seconds(),
    }
