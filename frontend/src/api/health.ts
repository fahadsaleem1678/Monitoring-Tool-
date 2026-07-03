export type BackendHealth = {
  status: string;
  service: string;
  started_at: string;
};

export async function getBackendHealth(): Promise<BackendHealth> {
  const response = await fetch("/api/v1/health");
  if (!response.ok) {
    throw new Error(`Backend health returned HTTP ${response.status}`);
  }
  return response.json() as Promise<BackendHealth>;
}
