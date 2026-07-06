import { authHeaders } from "./auth";

export type Dashboard = {
  id: string;
  title: string;
  description: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
  panels?: SavedPanel[];
};

export type SavedPanel = {
  id: string;
  dashboard_id: string;
  title: string;
  promql: string;
  visualization_type: string;
  grid_x: number;
  grid_y: number;
  grid_w: number;
  grid_h: number;
  refresh_interval_seconds: number;
  settings_json: Record<string, unknown>;
};

export type PanelInput = {
  title: string;
  promql: string;
  visualization_type: string;
  grid_x: number;
  grid_y: number;
  grid_w: number;
  grid_h: number;
  refresh_interval_seconds: number;
};

export async function listDashboards(token: string): Promise<Dashboard[]> {
  const response = await fetch("/api/v1/dashboards", { headers: authHeaders(token) });
  const body = await readJSON<{ dashboards: Dashboard[] }>(response);
  return body.dashboards;
}

export async function getDashboard(token: string, id: string): Promise<Dashboard> {
  const response = await fetch(`/api/v1/dashboards/${id}`, { headers: authHeaders(token) });
  const body = await readJSON<{ dashboard: Dashboard }>(response);
  return body.dashboard;
}

export async function createDashboard(token: string, title: string, description: string): Promise<Dashboard> {
  const response = await fetch("/api/v1/dashboards", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ title, description })
  });
  const body = await readJSON<{ dashboard: Dashboard }>(response);
  return body.dashboard;
}

export async function deleteDashboard(token: string, id: string): Promise<void> {
  const response = await fetch(`/api/v1/dashboards/${id}`, {
    method: "DELETE",
    headers: authHeaders(token)
  });
  await readJSON<{ ok: boolean }>(response);
}

export async function createPanel(token: string, dashboardId: string, input: PanelInput): Promise<SavedPanel> {
  const response = await fetch(`/api/v1/dashboards/${dashboardId}/panels`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify(input)
  });
  const body = await readJSON<{ panel: SavedPanel }>(response);
  return body.panel;
}

export async function deletePanel(token: string, id: string): Promise<void> {
  const response = await fetch(`/api/v1/panels/${id}`, {
    method: "DELETE",
    headers: authHeaders(token)
  });
  await readJSON<{ ok: boolean }>(response);
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json()) as T | { error?: string };
  if (!response.ok) {
    const message = typeof body === "object" && body !== null && "error" in body ? body.error : "";
    throw new Error(message || `HTTP ${response.status}`);
  }
  return body as T;
}
