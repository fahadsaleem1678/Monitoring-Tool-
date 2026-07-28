import { useEffect, useState } from "react";
import {
  approveIncident,
  broadcastIncident,
  getIncident,
  listIncidents,
  rejectIncident,
  updateIncidentDraft,
  type IncidentReview
} from "../../api/incidents";
import type { AuthUser } from "../../api/auth";

type Props = {
  token: string;
  user: AuthUser;
};

type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T }
  | { status: "error"; message: string };

export function IncidentReviewsView({ token, user }: Props) {
  const [incidents, setIncidents] = useState<Loadable<IncidentReview[]>>({ status: "loading" });
  const [selected, setSelected] = useState<Loadable<IncidentReview> | null>(null);
  const [draft, setDraft] = useState("");
  const [action, setAction] = useState<Loadable<string> | null>(null);
  const isAdmin = user.role === "admin";

  async function refreshList(selectFirst = false) {
    setIncidents({ status: "loading" });
    try {
      const data = await listIncidents(token);
      setIncidents({ status: "ready", data });
      if (selectFirst && data.length > 0) {
        await openIncident(data[0].id);
      }
    } catch (error) {
      setIncidents({ status: "error", message: error instanceof Error ? error.message : "Incident load failed" });
    }
  }

  async function openIncident(id: string) {
    setSelected({ status: "loading" });
    try {
      const incident = await getIncident(token, id);
      setSelected({ status: "ready", data: incident });
      setDraft(incident.final_message || incident.draft_message);
    } catch (error) {
      setSelected({ status: "error", message: error instanceof Error ? error.message : "Incident detail failed" });
    }
  }

  useEffect(() => {
    void refreshList(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function saveDraft() {
    if (selected?.status !== "ready") {
      return;
    }
    setAction({ status: "loading" });
    try {
      const incident = await updateIncidentDraft(token, selected.data.id, draft);
      setSelected({ status: "ready", data: incident });
      setAction({ status: "ready", data: "Draft saved" });
      await refreshList();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Draft save failed" });
    }
  }

  async function approveDraft() {
    if (selected?.status !== "ready") {
      return;
    }
    setAction({ status: "loading" });
    try {
      const incident = await approveIncident(token, selected.data.id, draft);
      setSelected({ status: "ready", data: incident });
      setAction({ status: "ready", data: "Incident approved" });
      await refreshList();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Approval failed" });
    }
  }

  async function rejectDraft() {
    if (selected?.status !== "ready") {
      return;
    }
    setAction({ status: "loading" });
    try {
      const incident = await rejectIncident(token, selected.data.id);
      setSelected({ status: "ready", data: incident });
      setAction({ status: "ready", data: "Incident rejected" });
      await refreshList();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Reject failed" });
    }
  }

  async function sendBroadcast() {
    if (selected?.status !== "ready") {
      return;
    }
    setAction({ status: "loading" });
    try {
      const incident = await broadcastIncident(token, selected.data.id);
      setSelected({ status: "ready", data: incident });
      setAction({ status: "ready", data: "Broadcast sent" });
      await refreshList();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Broadcast failed" });
    }
  }

  return (
    <section className="incidents-layout">
      <aside className="incidents-list">
        <header>
          <div>
            <h2>Incident Reviews</h2>
            <p>AI drafts waiting for engineer review</p>
          </div>
          <button type="button" onClick={() => void refreshList()}>
            Refresh
          </button>
        </header>

        {incidents.status === "loading" && <div className="panel-message compact">Loading incidents...</div>}
        {incidents.status === "error" && <div className="panel-message compact error">{incidents.message}</div>}
        {incidents.status === "ready" && incidents.data.length === 0 && (
          <div className="panel-message compact">No incident reviews yet</div>
        )}
        {incidents.status === "ready" &&
          incidents.data.map((incident) => (
            <button
              type="button"
              className="incident-list-item"
              key={incident.id}
              onClick={() => void openIncident(incident.id)}
            >
              <strong>{incident.title}</strong>
              <span>
                {incident.severity} · {incident.status}
              </span>
              <small>{new Date(incident.created_at).toLocaleString()}</small>
            </button>
          ))}
      </aside>

      <section className="incident-detail">
        {action?.status === "ready" && <span className="success-pill">{action.data}</span>}
        {action?.status === "error" && <span className="error-pill">{action.message}</span>}
        {!selected && <div className="panel-message">Select an incident review</div>}
        {selected?.status === "loading" && <div className="panel-message">Loading incident...</div>}
        {selected?.status === "error" && <div className="panel-message error">{selected.message}</div>}
        {selected?.status === "ready" && (
          <>
            <header className="detail-header">
              <div>
                <p className="eyebrow">
                  {selected.data.severity} · {selected.data.status}
                </p>
                <h2>{selected.data.title}</h2>
                <p>{selected.data.summary || "Investigation summary will appear here after the agent finishes."}</p>
              </div>
              <span className="panel-status ready">{selected.data.confidence || "pending"}</span>
            </header>

            <section className="incident-section">
              <h2>Slack Draft</h2>
              <textarea value={draft} onChange={(event) => setDraft(event.target.value)} disabled={!isAdmin} />
              {isAdmin && (
                <div className="incident-actions">
                  <button type="button" onClick={() => void saveDraft()}>
                    Save Draft
                  </button>
                  <button type="button" onClick={() => void approveDraft()}>
                    Approve
                  </button>
                  <button type="button" onClick={() => void sendBroadcast()}>
                    Broadcast
                  </button>
                  <button type="button" className="danger-button" onClick={() => void rejectDraft()}>
                    Reject
                  </button>
                </div>
              )}
            </section>

            <section className="incident-section">
              <h2>Reasoning Log</h2>
              {(selected.data.steps ?? []).length === 0 && (
                <div className="panel-message compact">No agent steps recorded yet</div>
              )}
              {(selected.data.steps ?? []).map((step) => (
                <article className="incident-step" key={step.id}>
                  <strong>{step.tool_name || step.step_type}</strong>
                  <code>{step.query_or_command}</code>
                  <p>{step.result_summary}</p>
                </article>
              ))}
            </section>

            <section className="incident-section">
              <h2>Audit Trail</h2>
              {(selected.data.audit_events ?? []).length === 0 && (
                <div className="panel-message compact">No audit events yet</div>
              )}
              {(selected.data.audit_events ?? []).map((event) => (
                <div className="audit-row" key={event.id}>
                  <strong>{event.action}</strong>
                  <span>{event.actor_type}</span>
                  <span>{new Date(event.created_at).toLocaleString()}</span>
                </div>
              ))}
            </section>
          </>
        )}
      </section>
    </section>
  );
}
