import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
  createAlertRule,
  deleteAlertRule,
  listAlertEvents,
  listAlertRules,
  testSlackNotification,
  type AlertEvent,
  type AlertRule,
  type AlertRuleInput
} from "../../api/alerts";
import type { AuthUser } from "../../api/auth";

type AlertsViewProps = {
  token: string;
  user: AuthUser;
};

type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T }
  | { status: "error"; message: string };

const defaultRule: AlertRuleInput = {
  name: "Targets down",
  promql: "sum(up == 0)",
  operator: ">",
  threshold: 0,
  for_seconds: 60,
  severity: "critical",
  enabled: true
};

export function AlertsView({ token, user }: AlertsViewProps) {
  const [rules, setRules] = useState<Loadable<AlertRule[]>>({ status: "loading" });
  const [events, setEvents] = useState<Loadable<AlertEvent[]>>({ status: "loading" });
  const [form, setForm] = useState<AlertRuleInput>(defaultRule);
  const [message, setMessage] = useState("Monitoring Tool test alert from Phase 4.");
  const [action, setAction] = useState<Loadable<string> | null>(null);
  const isAdmin = user.role === "admin";

  async function refresh() {
    await Promise.all([refreshRules(), refreshEvents()]);
  }

  async function refreshRules() {
    setRules({ status: "loading" });
    try {
      setRules({ status: "ready", data: await listAlertRules(token) });
    } catch (error) {
      setRules({ status: "error", message: error instanceof Error ? error.message : "Alert rules failed" });
    }
  }

  async function refreshEvents() {
    setEvents({ status: "loading" });
    try {
      setEvents({ status: "ready", data: await listAlertEvents(token) });
    } catch (error) {
      setEvents({ status: "error", message: error instanceof Error ? error.message : "Alert events failed" });
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function submitRule(event: FormEvent) {
    event.preventDefault();
    setAction({ status: "loading" });
    try {
      await createAlertRule(token, form);
      setAction({ status: "ready", data: "Alert rule saved" });
      setForm(defaultRule);
      await refreshRules();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Save failed" });
    }
  }

  async function removeRule(id: string) {
    setAction({ status: "loading" });
    try {
      await deleteAlertRule(token, id);
      setAction({ status: "ready", data: "Alert rule deleted" });
      await refreshRules();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Delete failed" });
    }
  }

  async function sendTestNotification() {
    setAction({ status: "loading" });
    try {
      await testSlackNotification(token, message);
      setAction({ status: "ready", data: "Slack notification sent" });
      await refreshEvents();
    } catch (error) {
      setAction({ status: "error", message: error instanceof Error ? error.message : "Slack notification failed" });
      await refreshEvents();
    }
  }

  return (
    <section className="alerts-layout">
      <div className="alerts-main">
        <header className="detail-header">
          <div>
            <h2>Alert Rules</h2>
            <p>Threshold rules and Slack notification tests</p>
          </div>
          {action?.status === "ready" && <span className="success-pill">{action.data}</span>}
          {action?.status === "error" && <span className="error-pill">{action.message}</span>}
        </header>

        {rules.status === "loading" && <div className="panel-message">Loading alert rules...</div>}
        {rules.status === "error" && <div className="panel-message error">{rules.message}</div>}
        {rules.status === "ready" && rules.data.length === 0 && <div className="panel-message">No alert rules yet</div>}
        {rules.status === "ready" && rules.data.length > 0 && (
          <div className="data-table">
            <div className="data-row table-head">
              <span>Name</span>
              <span>Condition</span>
              <span>Severity</span>
              <span>Status</span>
              <span></span>
            </div>
            {rules.data.map((rule) => (
              <div className="data-row" key={rule.id}>
                <strong>{rule.name}</strong>
                <span>
                  {rule.promql} {rule.operator} {rule.threshold}
                </span>
                <span>{rule.severity}</span>
                <span>{rule.enabled ? "Enabled" : "Disabled"}</span>
                <span>
                  {isAdmin && (
                    <button type="button" className="danger-button small" onClick={() => void removeRule(rule.id)}>
                      Delete
                    </button>
                  )}
                </span>
              </div>
            ))}
          </div>
        )}

        <section className="workbench-band">
          <header>
            <div>
              <h2>Recent Alert Events</h2>
              <p>Notification and alert history stored in PostgreSQL</p>
            </div>
            <button type="button" onClick={() => void refreshEvents()}>
              Refresh
            </button>
          </header>
          {events.status === "loading" && <div className="panel-message">Loading events...</div>}
          {events.status === "error" && <div className="panel-message error">{events.message}</div>}
          {events.status === "ready" && events.data.length === 0 && <div className="panel-message">No alert events yet</div>}
          {events.status === "ready" && events.data.length > 0 && (
            <div className="data-table">
              {events.data.map((event) => (
                <div className="data-row event-row" key={event.id}>
                  <strong>{event.status}</strong>
                  <span>{event.message}</span>
                  <span>{new Date(event.started_at).toLocaleString()}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {isAdmin && (
        <aside className="alerts-side">
          <form className="panel-form" onSubmit={submitRule}>
            <h2>Create Rule</h2>
            <label>
              Name
              <input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </label>
            <label>
              Query
              <textarea
                value={form.promql}
                onChange={(event) => setForm({ ...form, promql: event.target.value })}
                spellCheck={false}
              />
            </label>
            <div className="form-grid">
              <label>
                Operator
                <select value={form.operator} onChange={(event) => setForm({ ...form, operator: event.target.value })}>
                  <option value=">">&gt;</option>
                  <option value=">=">&gt;=</option>
                  <option value="<">&lt;</option>
                  <option value="<=">&lt;=</option>
                  <option value="==">==</option>
                  <option value="!=">!=</option>
                </select>
              </label>
              <label>
                Threshold
                <input
                  type="number"
                  value={form.threshold}
                  onChange={(event) => setForm({ ...form, threshold: Number(event.target.value) })}
                />
              </label>
              <label>
                For seconds
                <input
                  type="number"
                  value={form.for_seconds}
                  onChange={(event) => setForm({ ...form, for_seconds: Number(event.target.value) })}
                />
              </label>
              <label>
                Severity
                <select
                  value={form.severity}
                  onChange={(event) => setForm({ ...form, severity: event.target.value as AlertRuleInput["severity"] })}
                >
                  <option value="info">info</option>
                  <option value="warning">warning</option>
                  <option value="critical">critical</option>
                </select>
              </label>
            </div>
            <label className="checkbox-line">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
              />
              Enabled
            </label>
            <button type="submit">Save Rule</button>
          </form>

          <section className="panel-form">
            <h2>Slack Test</h2>
            <label>
              Message
              <textarea value={message} onChange={(event) => setMessage(event.target.value)} />
            </label>
            <button type="button" onClick={() => void sendTestNotification()}>
              Send Slack Test
            </button>
          </section>
        </aside>
      )}
    </section>
  );
}
