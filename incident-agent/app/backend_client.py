import httpx


class BackendClient:
    def __init__(self, base_url: str, agent_token: str):
        self.base_url = base_url.rstrip("/")
        self.headers = {"X-Agent-Token": agent_token}

    async def list_incidents(self) -> list[dict]:
        async with httpx.AsyncClient(timeout=10) as client:
            response = await client.get(f"{self.base_url}/api/v1/agent/incidents", headers=self.headers)
            response.raise_for_status()
            return response.json().get("incidents", [])

    async def claim(self, incident_id: str) -> dict:
        async with httpx.AsyncClient(timeout=10) as client:
            response = await client.post(f"{self.base_url}/api/v1/agent/incidents/{incident_id}/claim", headers=self.headers)
            response.raise_for_status()
            return response.json()["incident"]

    async def complete(self, incident_id: str, payload: dict) -> dict:
        async with httpx.AsyncClient(timeout=30) as client:
            response = await client.post(
                f"{self.base_url}/api/v1/agent/incidents/{incident_id}/complete",
                headers={**self.headers, "Content-Type": "application/json"},
                json=payload,
            )
            response.raise_for_status()
            return response.json()["incident"]
