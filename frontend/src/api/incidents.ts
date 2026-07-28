import { authHeaders } from "./auth";

export type IncidentStatus =
  | "pending_investigation"
  | "investigating"
  | "awaiting_review"
  | "approved"
  | "broadcasted"
  | "rejected"
  | "failed"
  | "closed";

export type IncidentReview = {
  id: string;
  alert_event_id?: string;
  alert_rule_id?: string;
  status: IncidentStatus;
  severity: string;
  title: string;
  summary: string;
  confidence: string;
  draft_message: string;
  final_message: string;
  approved_by?: string;
  approved_at?: string;
  broadcasted_at?: string;
  created_at: string;
  updated_at: string;
  steps?: IncidentInvestigationStep[];
  audit_events?: IncidentAuditEvent[];
};

export type IncidentInvestigationStep = {
  id: string;
  incident_review_id: string;
  step_type: string;
  tool_name: string;
  query_or_command: string;
  result_summary: string;
  raw_result_json: Record<string, unknown>;
  created_at: string;
};

export type IncidentAuditEvent = {
  id: string;
  incident_review_id: string;
  actor_type: string;
  actor_id?: string;
  action: string;
  details_json: Record<string, unknown>;
  created_at: string;
};

export async function listIncidents(token: string): Promise<IncidentReview[]> {
  const response = await fetch("/api/v1/incidents", { headers: authHeaders(token) });
  const body = await readJSON<{ incidents: IncidentReview[] | null }>(response);
  return body.incidents ?? [];
}

export async function getIncident(token: string, id: string): Promise<IncidentReview> {
  const response = await fetch(`/api/v1/incidents/${id}`, { headers: authHeaders(token) });
  const body = await readJSON<{ incident: IncidentReview }>(response);
  return body.incident;
}

export async function updateIncidentDraft(token: string, id: string, draftMessage: string): Promise<IncidentReview> {
  const response = await fetch(`/api/v1/incidents/${id}/draft`, {
    method: "PUT",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ draft_message: draftMessage })
  });
  const body = await readJSON<{ incident: IncidentReview }>(response);
  return body.incident;
}

export async function approveIncident(token: string, id: string, finalMessage: string): Promise<IncidentReview> {
  const response = await fetch(`/api/v1/incidents/${id}/approve`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ final_message: finalMessage })
  });
  const body = await readJSON<{ incident: IncidentReview }>(response);
  return body.incident;
}

export async function rejectIncident(token: string, id: string): Promise<IncidentReview> {
  const response = await fetch(`/api/v1/incidents/${id}/reject`, {
    method: "POST",
    headers: authHeaders(token)
  });
  const body = await readJSON<{ incident: IncidentReview }>(response);
  return body.incident;
}

export async function broadcastIncident(token: string, id: string): Promise<IncidentReview> {
  const response = await fetch(`/api/v1/incidents/${id}/broadcast`, {
    method: "POST",
    headers: authHeaders(token)
  });
  const body = await readJSON<{ incident: IncidentReview }>(response);
  return body.incident;
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json()) as T | { error?: string };
  if (!response.ok) {
    const message = typeof body === "object" && body !== null && "error" in body ? body.error : "";
    throw new Error(message || `HTTP ${response.status}`);
  }
  return body as T;
}
