import { useEffect, useState } from "react";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import {
  createDashboard,
  createPanel,
  deleteDashboard,
  deletePanel,
  getDashboard,
  listDashboards,
  type Dashboard,
  type PanelInput,
  type SavedPanel
} from "../../api/dashboards";
import { queryRange, type PrometheusMatrixResult } from "../../api/metrics";
import type { AuthUser } from "../../api/auth";
import { Panel } from "../overview/Panel";

type DashboardManagerProps = {
  token: string;
  user: AuthUser;
};

type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T }
  | { status: "error"; message: string };

export function DashboardManager({ token, user }: DashboardManagerProps) {
  const [dashboards, setDashboards] = useState<Loadable<Dashboard[]>>({ status: "loading" });
  const [selected, setSelected] = useState<Loadable<Dashboard> | null>(null);
  const [form, setForm] = useState({ title: "New Kubernetes Dashboard", description: "" });
  const [panelForm, setPanelForm] = useState<PanelInput>(defaultPanelInput);
  const [panelData, setPanelData] = useState<Record<string, Loadable<PrometheusMatrixResult[]>>>({});
  const isAdmin = user.role === "admin";

  async function refreshList() {
    setDashboards({ status: "loading" });
    try {
      const items = await listDashboards(token);
      setDashboards({ status: "ready", data: items });
      if (!selected && items.length > 0) {
        await openDashboard(items[0].id);
      }
    } catch (error) {
      setDashboards({ status: "error", message: error instanceof Error ? error.message : "Load failed" });
    }
  }

  async function openDashboard(id: string) {
    setSelected({ status: "loading" });
    try {
      const dashboard = await getDashboard(token, id);
      setSelected({ status: "ready", data: dashboard });
      setPanelData({});
      for (const panel of dashboard.panels ?? []) {
        void loadSavedPanel(panel, setPanelData);
      }
    } catch (error) {
      setSelected({ status: "error", message: error instanceof Error ? error.message : "Dashboard load failed" });
    }
  }

  useEffect(() => {
    void refreshList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function submitDashboard(event: FormEvent) {
    event.preventDefault();
    const dashboard = await createDashboard(token, form.title, form.description);
    setForm({ title: "New Kubernetes Dashboard", description: "" });
    await refreshList();
    await openDashboard(dashboard.id);
  }

  async function submitPanel(event: FormEvent) {
    event.preventDefault();
    if (selected?.status !== "ready") {
      return;
    }
    await createPanel(token, selected.data.id, panelForm);
    setPanelForm(defaultPanelInput);
    await openDashboard(selected.data.id);
    await refreshList();
  }

  async function removeDashboard(id: string) {
    await deleteDashboard(token, id);
    setSelected(null);
    await refreshList();
  }

  async function removePanel(id: string) {
    if (selected?.status !== "ready") {
      return;
    }
    await deletePanel(token, id);
    await openDashboard(selected.data.id);
  }

  return (
    <section className="dashboards-layout">
      <aside className="dashboard-sidebar">
        <header>
          <h2>Dashboards</h2>
          <span>{dashboards.status === "ready" ? dashboards.data.length : "-"}</span>
        </header>
        {dashboards.status === "loading" && <p>Loading dashboards...</p>}
        {dashboards.status === "error" && <p className="error">{dashboards.message}</p>}
        {dashboards.status === "ready" && dashboards.data.length === 0 && <p>No dashboards yet</p>}
        {dashboards.status === "ready" && (
          <div className="dashboard-list">
            {dashboards.data.map((dashboard) => (
              <button key={dashboard.id} type="button" onClick={() => void openDashboard(dashboard.id)}>
                <strong>{dashboard.title}</strong>
                <span>{new Date(dashboard.updated_at).toLocaleString()}</span>
              </button>
            ))}
          </div>
        )}

        {isAdmin && (
          <form className="compact-form" onSubmit={submitDashboard}>
            <h3>Create Dashboard</h3>
            <input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} />
            <textarea
              value={form.description}
              onChange={(event) => setForm({ ...form, description: event.target.value })}
              placeholder="Description"
            />
            <button type="submit">Create</button>
          </form>
        )}
      </aside>

      <div className="dashboard-detail">
        {selected == null && <div className="panel-message">Select or create a dashboard</div>}
        {selected?.status === "loading" && <div className="panel-message">Loading dashboard...</div>}
        {selected?.status === "error" && <div className="panel-message error">{selected.message}</div>}
        {selected?.status === "ready" && (
          <>
            <header className="detail-header">
              <div>
                <h2>{selected.data.title}</h2>
                <p>{selected.data.description || "Saved dashboard"}</p>
              </div>
              {isAdmin && (
                <button type="button" className="danger-button" onClick={() => void removeDashboard(selected.data.id)}>
                  Delete
                </button>
              )}
            </header>

            <section className="dashboard-grid">
              {(selected.data.panels ?? []).map((panel) => {
                const state = panelData[panel.id] ?? { status: "loading" as const };
                return (
                  <div className="saved-panel-wrap" key={panel.id}>
                    {isAdmin && (
                      <button type="button" className="panel-delete" onClick={() => void removePanel(panel.id)}>
                        Delete
                      </button>
                    )}
                    <Panel
                      title={panel.title}
                      subtitle={panel.promql}
                      status={state.status}
                      error={state.status === "error" ? state.message : undefined}
                      series={state.status === "ready" ? matrixToSeries(state.data) : []}
                    />
                  </div>
                );
              })}
            </section>

            {isAdmin && (
              <form className="panel-form" onSubmit={submitPanel}>
                <h2>Add Panel</h2>
                <input
                  value={panelForm.title}
                  onChange={(event) => setPanelForm({ ...panelForm, title: event.target.value })}
                  placeholder="Panel title"
                />
                <textarea
                  value={panelForm.promql}
                  onChange={(event) => setPanelForm({ ...panelForm, promql: event.target.value })}
                  placeholder="PromQL"
                  spellCheck={false}
                />
                <button type="submit">Add Panel</button>
              </form>
            )}
          </>
        )}
      </div>
    </section>
  );
}

const defaultPanelInput: PanelInput = {
  title: "Targets Up",
  promql: "sum(up)",
  visualization_type: "line",
  grid_x: 0,
  grid_y: 0,
  grid_w: 6,
  grid_h: 4,
  refresh_interval_seconds: 30
};

async function loadSavedPanel(
  panel: SavedPanel,
  setPanelData: Dispatch<SetStateAction<Record<string, Loadable<PrometheusMatrixResult[]>>>>
) {
  try {
    const data = await queryRange(panel.promql, 60 * 60, Math.max(panel.refresh_interval_seconds, 30));
    setPanelData((current) => ({ ...current, [panel.id]: { status: "ready", data: data.result } }));
  } catch (error) {
    setPanelData((current) => ({
      ...current,
      [panel.id]: { status: "error", message: error instanceof Error ? error.message : "Panel query failed" }
    }));
  }
}

function matrixToSeries(results: PrometheusMatrixResult[]) {
  return results.slice(0, 6).map((result, index) => ({
    label: result.metric.node ?? result.metric.instance ?? result.metric.namespace ?? `series ${index + 1}`,
    points: result.values.map(([timestamp, value]) => [timestamp, Number(value)] as [number, number])
  }));
}
