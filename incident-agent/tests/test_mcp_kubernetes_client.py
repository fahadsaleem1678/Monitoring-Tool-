import pytest

from app.mcp_kubernetes_client import FailoverKubernetesMCPClient, OfficialKubernetesMCPClient, _parse_yamlish_items


def test_parse_yamlish_items_reads_resource_list():
    text = """
apiVersion: v1
items:
  - metadata:
      namespace: monitoring-tool
      name: broken-api-123
    status:
      phase: Running
      containerStatuses:
        - restartCount: 5
          state:
            waiting:
              reason: CrashLoopBackOff
kind: List
"""

    items = _parse_yamlish_items(text)

    assert items[0]["metadata"]["name"] == "broken-api-123"
    assert items[0]["status"]["containerStatuses"][0]["state"]["waiting"]["reason"] == "CrashLoopBackOff"


@pytest.mark.anyio
async def test_kubernetes_failover_uses_mvp_when_official_pods_are_unparsed():
    class EmptyOfficial:
        async def pods(self, namespace: str = ""):
            return {"tool": "official-kubernetes.resources_list", "summary": "empty", "data": {"pods": []}}

    class WorkingFallback:
        async def pods(self, namespace: str = ""):
            return {"tool": "kubernetes.pods", "summary": "fallback pods", "data": {"pods": [{"name": "broken-api"}]}}

    client = FailoverKubernetesMCPClient(EmptyOfficial(), WorkingFallback())

    result = await client.pods("monitoring-tool")

    assert result["summary"] == "fallback pods (official MCP fallback used)"
    assert result["official_mcp_fallback"]["fallback_tool"] == "kubernetes.pods"


def test_official_pod_list_parses_full_text_not_preview():
    full_text = "\n".join(
        f"""
- apiVersion: v1
  kind: Pod
  metadata:
    namespace: monitoring-tool
    name: pod-{index}
  status:
    phase: Running
"""
        for index in range(7)
    )
    client = OfficialKubernetesMCPClient("http://official")
    result = {
        "tool": "official-kubernetes.pods_list_in_namespace",
        "summary": "completed",
        "data": {"_text": full_text, "text_preview": full_text[:2000]},
    }

    parsed = client._pod_list_result(result)

    assert parsed["summary"] == "Kubernetes returned 7 pod(s) via official MCP"
    assert parsed["data"]["pods"][-1]["name"] == "pod-6"
    assert "_text" not in parsed["data"]
