import httpx


class PrometheusMCPClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    async def instant_query(self, query: str) -> dict:
        async with httpx.AsyncClient(timeout=10) as client:
            response = await client.post(f"{self.base_url}/tools/prometheus.query", json={"query": query})
            response.raise_for_status()
            return response.json()


class KubernetesMCPClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    async def pods(self, namespace: str = "") -> dict:
        async with httpx.AsyncClient(timeout=10) as client:
            response = await client.post(f"{self.base_url}/tools/kubernetes.pods", json={"namespace": namespace})
            response.raise_for_status()
            return response.json()
