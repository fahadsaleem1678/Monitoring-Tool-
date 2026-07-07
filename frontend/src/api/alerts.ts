import { authHeaders } from "./auth";

export type AlertRule = {
  id: string;
  name: string;
  promql: string;
  operator: string;
  threshold: number;
  for_seconds: number;
  severity: "info" | "warning" | "critical";
  enabled: boolean;
  created_at: string;
  updated_at: string;
};

export type AlertEvent = {
  id: string;
  rule_id?: string;
  status: string;
  value?: number;
  message: string;
  started_at: string;
  resolved_at?: string;
};

export type AlertRuleInput = {
  name: string;
  promql: string;
  operator: string;
  threshold: number;
  for_seconds: number;
  severity: "info" | "warning" | "critical";
  enabled: boolean;
};

export async function listAlertRules(token: string): Promise<AlertRule[]> {
  const response = await fetch("/api/v1/alerts/rules", { headers: authHeaders(token) });
  const body = await readJSON<{ rules: AlertRule[] | null }>(response);
  return body.rules ?? [];
}

export async function createAlertRule(token: string, input: AlertRuleInput): Promise<AlertRule> {
  const response = await fetch("/api/v1/alerts/rules", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify(input)
  });
  const body = await readJSON<{ rule: AlertRule }>(response);
  return body.rule;
}

export async function deleteAlertRule(token: string, id: string): Promise<void> {
  const response = await fetch(`/api/v1/alerts/rules/${id}`, {
    method: "DELETE",
    headers: authHeaders(token)
  });
  await readJSON<{ ok: boolean }>(response);
}

export async function listAlertEvents(token: string): Promise<AlertEvent[]> {
  const response = await fetch("/api/v1/alerts/events", { headers: authHeaders(token) });
  const body = await readJSON<{ events: AlertEvent[] | null }>(response);
  return body.events ?? [];
}

export async function testSlackNotification(token: string, message: string): Promise<AlertEvent> {
  const response = await fetch("/api/v1/alerts/test-notification", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ message })
  });
  const body = await readJSON<{ event: AlertEvent }>(response);
  return body.event;
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json()) as T | { error?: string };
  if (!response.ok) {
    const message = typeof body === "object" && body !== null && "error" in body ? body.error : "";
    throw new Error(message || `HTTP ${response.status}`);
  }
  return body as T;
}
