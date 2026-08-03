import pytest
import httpx

from app.mcp_prometheus_client import (
    FailoverPrometheusMCPClient,
    OfficialPrometheusMCPClient,
    PrometheusMCPClient,
    _compact_official_result,
    _decode_mcp_response,
    prometheus_client_from_env,
)


def test_prometheus_client_from_env_defaults_to_mvp():
    client = prometheus_client_from_env("mvp", "http://mvp", "http://official")

    assert isinstance(client, PrometheusMCPClient)
    assert client.base_url == "http://mvp"


def test_prometheus_client_from_env_can_select_official():
    client = prometheus_client_from_env("official", "http://mvp", "http://official")

    assert isinstance(client, FailoverPrometheusMCPClient)
    assert client.primary.base_url == "http://official"
    assert client.fallback.base_url == "http://mvp"


def test_decode_mcp_json_response():
    response = httpx.Response(200, json={"jsonrpc": "2.0", "result": {"ok": True}})

    assert _decode_mcp_response(response)["result"]["ok"] is True


def test_decode_mcp_event_stream_response():
    response = httpx.Response(
        200,
        headers={"content-type": "text/event-stream"},
        text='event: message\ndata: {"jsonrpc":"2.0","result":{"ok":true}}\n\n',
    )

    assert _decode_mcp_response(response)["result"]["ok"] is True


def test_compact_official_result_keeps_series_summary_without_full_duplicate_text():
    rows = [
        {"metric": {"pod": f"broken-api-{index}", "namespace": "monitoring-tool"}, "value": [123, "1"]}
        for index in range(20)
    ]
    text = '{"resultType":"vector","result":' + ("x" * 5000)
    result = {
        "content": [{"type": "text", "text": text}],
        "structuredContent": {"resultType": "vector", "result": rows},
        "isError": False,
    }

    compact = _compact_official_result(result, text)

    assert compact["series_count"] == 20
    assert len(compact["result"]) == 10
    assert compact["truncated"] is True
    assert len(compact["text_preview"]) == 1000


@pytest.mark.anyio
async def test_official_client_retries_mcp_trailing_slash():
    calls = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(request.url.path)
        if request.url.path == "/mcp":
            return httpx.Response(404, json={"error": "not found"})
        return httpx.Response(200, json={"jsonrpc": "2.0", "result": {"tools": [{"name": "execute_query"}]}})

    transport = httpx.MockTransport(handler)
    original_async_client = httpx.AsyncClient

    class MockAsyncClient(httpx.AsyncClient):
        def __init__(self, *args, **kwargs):
            kwargs["transport"] = transport
            super().__init__(*args, **kwargs)

    httpx.AsyncClient = MockAsyncClient
    try:
        client = OfficialPrometheusMCPClient("http://official")

        result = await client.tools_list()
    finally:
        httpx.AsyncClient = original_async_client

    assert calls == ["/mcp", "/mcp/"]
    assert result["summary"] == "Official Prometheus MCP exposed 1 tools"


@pytest.mark.anyio
async def test_failover_uses_mvp_when_official_fails():
    class BrokenOfficial:
        async def instant_query(self, query: str):
            raise RuntimeError("official down")

    class WorkingFallback:
        async def instant_query(self, query: str):
            return {"tool": "prometheus.query", "summary": "fallback ok", "data": {"query": query}}

    client = FailoverPrometheusMCPClient(BrokenOfficial(), WorkingFallback())

    result = await client.instant_query("up")

    assert result["summary"] == "fallback ok (official MCP fallback used)"
    assert result["official_mcp_fallback"]["reason"] == "official down"
