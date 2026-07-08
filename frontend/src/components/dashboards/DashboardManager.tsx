import { useEffect, useState } from "react";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import {
  createDashboard,
  createPanel,
  deleteDashboard,
  deletePanel,
  getDashboard,
  listDashboards,
  updatePanel,
  type Dashboard,
  type PanelInput,
  type SavedPanel
} from "../../api/dashboards";
import { connectLivePanels, type LiveMessage } from "../../api/live";
import { listLabelValues, queryRange, type PrometheusMatrixResult, type PrometheusVectorResult } from "../../api/metrics";
import type { AuthUser } from "../../api/auth";
import { Panel } from "../overview/Panel";
import { VisualQueryBuilder } from "../query/VisualQueryBuilder";

type DashboardManagerProps = {
  token: string;
  user: AuthUser;
};

type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T }
  | { status: "error"; message: string };

type DashboardVariable = {
  id: string;
  name: string;
  label: string;
  value: string;
};

const timeRangeOptions = [
  { label: "Last 15m", seconds: 15 * 60 },
  { label: "Last 1h", seconds: 60 * 60 },
  { label: "Last 6h", seconds: 6 * 60 * 60 },
  { label: "Last 24h", seconds: 24 * 60 * 60 }
];

const refreshOptions = [
  { label: "Off", seconds: 0 },
  { label: "15s", seconds: 15 },
  { label: "30s", seconds: 30 },
  { label: "1m", seconds: 60 },
  { label: "5m", seconds: 5 * 60 }
];

export function DashboardManager({ token, user }: DashboardManagerProps) {
  const [dashboards, setDashboards] = useState<Loadable<Dashboard[]>>({ status: "loading" });
  const [selected, setSelected] = useState<Loadable<Dashboard> | null>(null);
  const [form, setForm] = useState({ title: "New Kubernetes Dashboard", description: "" });
  const [panelForm, setPanelForm] = useState<PanelInput>(defaultPanelInput);
  const [editingPanel, setEditingPanel] = useState<{ id: string; input: PanelInput } | null>(null);
  const [panelData, setPanelData] = useState<Record<string, Loadable<PrometheusMatrixResult[]>>>({});
  const [variables, setVariables] = useState<DashboardVariable[]>([]);
  const [timeRangeSeconds, setTimeRangeSeconds] = useState(60 * 60);
  const [dashboardRefreshSeconds, setDashboardRefreshSeconds] = useState(30);
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);
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
      setEditingPanel(null);
      setPanelData({});
      const nextVariables = loadDashboardVariables(dashboard);
      setVariables(nextVariables);
      const nextValues = variableValueMap(nextVariables);
      setLastRefreshed(new Date());
      for (const panel of dashboard.panels ?? []) {
        void loadSavedPanel(panel, setPanelData, nextValues, timeRangeSeconds);
      }
    } catch (error) {
      setSelected({ status: "error", message: error instanceof Error ? error.message : "Dashboard load failed" });
    }
  }

  useEffect(() => {
    void refreshList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  useEffect(() => {
    if (selected?.status !== "ready" || (selected.data.panels ?? []).length === 0) {
      return;
    }

    const panels = selected.data.panels ?? [];
    let closed = false;
    let reconnectTimer: number | undefined;
    let fallbackTimer: number | undefined;
    let stopSocket: (() => void) | undefined;

    const startFallback = () => {
      if (fallbackTimer != null) {
        return;
      }
      fallbackTimer = window.setInterval(() => {
        for (const panel of panels) {
          void loadSavedPanel(panel, setPanelData, variableValueMap(variables), timeRangeSeconds);
        }
      }, 30_000);
    };

    const stopFallback = () => {
      window.clearInterval(fallbackTimer);
      fallbackTimer = undefined;
    };

    const startSocket = () => {
      stopSocket = connectLivePanels(
        token,
        panels.map((panel) => ({
          panel_id: panel.id,
          promql: substituteVariables(panel.promql, variableValueMap(variables)),
          refresh_interval_seconds: panel.refresh_interval_seconds
        })),
        {
          onMessage: handleLiveMessage,
          onDisconnect: () => {
            if (closed) {
              return;
            }
            startFallback();
            if (reconnectTimer == null) {
              reconnectTimer = window.setTimeout(() => {
                reconnectTimer = undefined;
                startSocket();
              }, 5_000);
            }
          }
        }
      );
    };

    const handleLiveMessage = (message: LiveMessage) => {
      if (message.type === "metric_update") {
        stopFallback();
        setPanelData((current) => appendVectorUpdate(current, message.panel_id, message.data.result, message.timestamp));
        return;
      }
      if (message.type === "error" && message.panel_id) {
        const panelID = message.panel_id;
        setPanelData((current) => ({
          ...current,
          [panelID]: { status: "error", message: message.message }
        }));
      }
    };

    startSocket();

    return () => {
      closed = true;
      stopSocket?.();
      window.clearTimeout(reconnectTimer);
      window.clearInterval(fallbackTimer);
    };
  }, [selected, token, variables, timeRangeSeconds]);

  useEffect(() => {
    if (selected?.status !== "ready") {
      return;
    }
    saveDashboardVariables(selected.data.id, variables);
    reloadDashboardPanels(selected.data.panels ?? []);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [variables, timeRangeSeconds]);

  useEffect(() => {
    if (selected?.status !== "ready" || dashboardRefreshSeconds <= 0) {
      return;
    }

    const timer = window.setInterval(() => {
      reloadDashboardPanels(selected.data.panels ?? []);
    }, dashboardRefreshSeconds * 1000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, variables, timeRangeSeconds, dashboardRefreshSeconds]);

  function reloadDashboardPanels(panels: SavedPanel[]) {
    setPanelData({});
    setLastRefreshed(new Date());
    const values = variableValueMap(variables);
    for (const panel of panels) {
      void loadSavedPanel(panel, setPanelData, values, timeRangeSeconds);
    }
  }

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

  async function submitPanelEdit(event: FormEvent) {
    event.preventDefault();
    if (selected?.status !== "ready" || editingPanel == null) {
      return;
    }
    await updatePanel(token, editingPanel.id, editingPanel.input);
    setEditingPanel(null);
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

            <DashboardTimeControls
              timeRangeSeconds={timeRangeSeconds}
              refreshSeconds={dashboardRefreshSeconds}
              lastRefreshed={lastRefreshed}
              onTimeRangeChange={setTimeRangeSeconds}
              onRefreshChange={setDashboardRefreshSeconds}
              onRefreshNow={() => reloadDashboardPanels(selected.data.panels ?? [])}
            />

            <DashboardVariables
              dashboardID={selected.data.id}
              panels={selected.data.panels ?? []}
              variables={variables}
              onChange={setVariables}
            />

            <section className="dashboard-grid">
              {(selected.data.panels ?? []).map((panel) => {
                const state = panelData[panel.id] ?? { status: "loading" as const };
                return (
                  <div className="saved-panel-wrap" key={panel.id}>
                    {isAdmin && (
                      <button
                        type="button"
                        className="panel-delete"
                        onClick={(event) => {
                          event.stopPropagation();
                          void removePanel(panel.id);
                        }}
                      >
                        Delete
                      </button>
                    )}
                    <div
                      className={isAdmin ? "panel-click-target" : undefined}
                      role={isAdmin ? "button" : undefined}
                      tabIndex={isAdmin ? 0 : undefined}
                      onClick={() => {
                        if (isAdmin) {
                          setEditingPanel({ id: panel.id, input: panelToInput(panel) });
                        }
                      }}
                      onKeyDown={(event) => {
                        if (isAdmin && (event.key === "Enter" || event.key === " ")) {
                          event.preventDefault();
                          setEditingPanel({ id: panel.id, input: panelToInput(panel) });
                        }
                      }}
                    >
                      <Panel
                        title={panel.title}
                        subtitle={panel.promql}
                        status={state.status}
                        error={state.status === "error" ? state.message : undefined}
                        series={state.status === "ready" ? matrixToSeries(state.data) : []}
                        unit={unitFromSettings(panel.settings_json)}
                        visualizationType={panel.visualization_type}
                        gaugeMax={gaugeMaxFromSettings(panel.settings_json)}
                      />
                    </div>
                  </div>
                );
              })}
            </section>

            {isAdmin && (
              <form className="panel-form" onSubmit={submitPanelEdit}>
                <header className="panel-form-header">
                  <h2>Edit Panel</h2>
                  {editingPanel && (
                    <button type="button" className="secondary-button" onClick={() => setEditingPanel(null)}>
                      Cancel
                    </button>
                  )}
                </header>
                {editingPanel == null && <div className="panel-message compact">Click a panel to edit it</div>}
                {editingPanel != null && (
                  <>
                    <input
                      value={editingPanel.input.title}
                      onChange={(event) =>
                        setEditingPanel({
                          ...editingPanel,
                          input: { ...editingPanel.input, title: event.target.value }
                        })
                      }
                      placeholder="Panel title"
                    />
                    <textarea
                      value={editingPanel.input.promql}
                      onChange={(event) =>
                        setEditingPanel({
                          ...editingPanel,
                          input: { ...editingPanel.input, promql: event.target.value }
                        })
                      }
                      placeholder="PromQL"
                      spellCheck={false}
                    />
                    <VisualQueryBuilder
                      value={editingPanel.input.promql}
                      onApply={(promql) =>
                        setEditingPanel({
                          ...editingPanel,
                          input: { ...editingPanel.input, promql }
                        })
                      }
                    />
                    <PanelSettingsFields
                      panel={editingPanel.input}
                      onChange={(input) => setEditingPanel({ ...editingPanel, input })}
                    />
                    <button type="submit">Save Panel</button>
                  </>
                )}
              </form>
            )}

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
                <VisualQueryBuilder value={panelForm.promql} onApply={(promql) => setPanelForm({ ...panelForm, promql })} />
                <PanelSettingsFields panel={panelForm} onChange={setPanelForm} />
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
  refresh_interval_seconds: 30,
  settings_json: {
    unit: "",
    gauge_max: 100
  }
};

function DashboardTimeControls({
  timeRangeSeconds,
  refreshSeconds,
  lastRefreshed,
  onTimeRangeChange,
  onRefreshChange,
  onRefreshNow
}: {
  timeRangeSeconds: number;
  refreshSeconds: number;
  lastRefreshed: Date | null;
  onTimeRangeChange: (seconds: number) => void;
  onRefreshChange: (seconds: number) => void;
  onRefreshNow: () => void;
}) {
  return (
    <section className="dashboard-time-controls" aria-label="Dashboard time controls">
      <div className="time-control-group">
        <label>
          Time range
          <select value={timeRangeSeconds} onChange={(event) => onTimeRangeChange(Number(event.target.value))}>
            {timeRangeOptions.map((option) => (
              <option key={option.seconds} value={option.seconds}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Auto refresh
          <select value={refreshSeconds} onChange={(event) => onRefreshChange(Number(event.target.value))}>
            {refreshOptions.map((option) => (
              <option key={option.seconds} value={option.seconds}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="time-control-actions">
        <span>{lastRefreshed ? `Last refresh ${lastRefreshed.toLocaleTimeString()}` : "Not refreshed yet"}</span>
        <button type="button" onClick={onRefreshNow}>
          Refresh
        </button>
      </div>
    </section>
  );
}

function DashboardVariables({
  dashboardID,
  panels,
  variables,
  onChange
}: {
  dashboardID: string;
  panels: SavedPanel[];
  variables: DashboardVariable[];
  onChange: (variables: DashboardVariable[]) => void;
}) {
  const [options, setOptions] = useState<Record<string, string[]>>({});
  const placeholders = Array.from(new Set(panels.flatMap((panel) => variableNamesFromQuery(panel.promql))));

  function addVariable(name = "namespace") {
    const normalized = cleanVariableName(name);
    if (variables.some((item) => item.name === normalized)) {
      return;
    }
    onChange([...variables, { id: `${Date.now()}-${normalized}`, name: normalized, label: normalized, value: "" }]);
  }

  function updateVariable(id: string, patch: Partial<DashboardVariable>) {
    onChange(
      variables.map((variable) =>
        variable.id === id
          ? {
              ...variable,
              ...patch,
              name: patch.name != null ? cleanVariableName(patch.name) : variable.name
            }
          : variable
      )
    );
  }

  function removeVariable(id: string) {
    onChange(variables.filter((variable) => variable.id !== id));
  }

  function loadOptions(label: string) {
    if (options[label]) {
      return;
    }
    void listLabelValues(label)
      .then((values) => setOptions((current) => ({ ...current, [label]: values.slice(0, 160) })))
      .catch(() => setOptions((current) => ({ ...current, [label]: [] })));
  }

  return (
    <section className="dashboard-variables" aria-label="Dashboard variables">
      <header>
        <div>
          <h3>Template Variables</h3>
          <p>Use names like $namespace in panel PromQL.</p>
        </div>
        <button type="button" onClick={() => addVariable()}>
          Add variable
        </button>
      </header>

      {placeholders.length > 0 && (
        <div className="variable-suggestions">
          {placeholders.map((name) => (
            <button key={`${dashboardID}-${name}`} type="button" onClick={() => addVariable(name)}>
              ${name}
            </button>
          ))}
        </div>
      )}

      {variables.length === 0 && <div className="builder-empty">No variables defined</div>}
      {variables.map((variable) => (
        <div className="variable-row" key={variable.id}>
          <label>
            Name
            <input value={variable.name} onChange={(event) => updateVariable(variable.id, { name: event.target.value })} />
          </label>
          <label>
            Source label
            <input
              value={variable.label}
              onFocus={() => loadOptions(variable.label)}
              onChange={(event) => {
                updateVariable(variable.id, { label: event.target.value, value: "" });
                loadOptions(event.target.value);
              }}
              placeholder="namespace"
            />
          </label>
          <label>
            Value
            <select
              value={variable.value}
              onFocus={() => loadOptions(variable.label)}
              onChange={(event) => updateVariable(variable.id, { value: event.target.value })}
            >
              <option value="">All / empty</option>
              {(options[variable.label] ?? []).map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className="danger-button small" onClick={() => removeVariable(variable.id)}>
            Remove
          </button>
        </div>
      ))}
    </section>
  );
}

function PanelSettingsFields({ panel, onChange }: { panel: PanelInput; onChange: (panel: PanelInput) => void }) {
  return (
    <div className="form-grid">
      <label>
        Visualization
        <select value={panel.visualization_type} onChange={(event) => onChange({ ...panel, visualization_type: event.target.value })}>
          <option value="line">Line graph</option>
          <option value="bar">Bar chart</option>
          <option value="gauge">Gauge</option>
        </select>
      </label>
      <label>
        Unit
        <select
          value={String(panel.settings_json?.unit ?? "")}
          onChange={(event) =>
            onChange({
              ...panel,
              settings_json: { ...(panel.settings_json ?? {}), unit: event.target.value }
            })
          }
        >
          <option value="">Number</option>
          <option value="%">Percent</option>
        </select>
      </label>
      <label>
        Gauge max
        <input
          type="number"
          value={String(panel.settings_json?.gauge_max ?? 100)}
          onChange={(event) =>
            onChange({
              ...panel,
              settings_json: { ...(panel.settings_json ?? {}), gauge_max: Number(event.target.value) }
            })
          }
        />
      </label>
      <label>
        Refresh seconds
        <input
          type="number"
          value={panel.refresh_interval_seconds}
          onChange={(event) => onChange({ ...panel, refresh_interval_seconds: Number(event.target.value) })}
        />
      </label>
    </div>
  );
}

function panelToInput(panel: SavedPanel): PanelInput {
  return {
    title: panel.title,
    promql: panel.promql,
    visualization_type: panel.visualization_type,
    grid_x: panel.grid_x,
    grid_y: panel.grid_y,
    grid_w: panel.grid_w,
    grid_h: panel.grid_h,
    refresh_interval_seconds: panel.refresh_interval_seconds,
    settings_json: panel.settings_json ?? {}
  };
}

async function loadSavedPanel(
  panel: SavedPanel,
  setPanelData: Dispatch<SetStateAction<Record<string, Loadable<PrometheusMatrixResult[]>>>>,
  variables: Record<string, string> = {},
  rangeSeconds = 60 * 60
) {
  try {
    const stepSeconds = Math.max(panel.refresh_interval_seconds, Math.ceil(rangeSeconds / 240), 15);
    const data = await queryRange(substituteVariables(panel.promql, variables), rangeSeconds, stepSeconds);
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

function appendVectorUpdate(
  current: Record<string, Loadable<PrometheusMatrixResult[]>>,
  panelID: string,
  results: PrometheusVectorResult[],
  timestamp: number
): Record<string, Loadable<PrometheusMatrixResult[]>> {
  const previous = current[panelID];
  const byMetric = new Map<string, PrometheusMatrixResult>();

  if (previous?.status === "ready") {
    for (const series of previous.data) {
      byMetric.set(metricKey(series.metric), { metric: series.metric, values: [...series.values] });
    }
  }

  for (const result of results) {
    const key = metricKey(result.metric);
    const series = byMetric.get(key) ?? { metric: result.metric, values: [] };
    const point: [number, string] = [timestamp, result.value[1]];
    series.values = [...series.values, point].slice(-120);
    byMetric.set(key, series);
  }

  return {
    ...current,
    [panelID]: { status: "ready", data: [...byMetric.values()] }
  };
}

function metricKey(metric: Record<string, string>) {
  return JSON.stringify(Object.entries(metric).sort(([left], [right]) => left.localeCompare(right)));
}

function unitFromSettings(settings: Record<string, unknown> | undefined) {
  return typeof settings?.unit === "string" ? settings.unit : "";
}

function gaugeMaxFromSettings(settings: Record<string, unknown> | undefined) {
  const value = Number(settings?.gauge_max);
  return Number.isFinite(value) && value > 0 ? value : undefined;
}

function variableStorageKey(dashboardID: string) {
  return `monitoring-tool.dashboard.${dashboardID}.variables`;
}

function loadDashboardVariables(dashboard: Dashboard): DashboardVariable[] {
  const saved = window.localStorage.getItem(variableStorageKey(dashboard.id));
  const savedVariables = saved ? parseSavedVariables(saved) : [];
  const placeholders = Array.from(new Set((dashboard.panels ?? []).flatMap((panel) => variableNamesFromQuery(panel.promql))));
  const merged = [...savedVariables];
  for (const name of placeholders) {
    if (!merged.some((variable) => variable.name === name)) {
      merged.push({ id: `${dashboard.id}-${name}`, name, label: name, value: "" });
    }
  }
  return merged;
}

function saveDashboardVariables(dashboardID: string, variables: DashboardVariable[]) {
  window.localStorage.setItem(variableStorageKey(dashboardID), JSON.stringify(variables));
}

function parseSavedVariables(value: string): DashboardVariable[] {
  try {
    const parsed = JSON.parse(value) as DashboardVariable[];
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .filter((item) => typeof item.name === "string" && typeof item.label === "string")
      .map((item, index) => ({
        id: typeof item.id === "string" ? item.id : `${Date.now()}-${index}`,
        name: cleanVariableName(item.name),
        label: item.label,
        value: typeof item.value === "string" ? item.value : ""
      }));
  } catch {
    return [];
  }
}

function variableNamesFromQuery(query: string) {
  const matches = query.matchAll(/\$([a-zA-Z_][a-zA-Z0-9_]*)/g);
  return Array.from(matches, (match) => match[1]);
}

function variableValueMap(variables: DashboardVariable[]) {
  return Object.fromEntries(variables.map((variable) => [variable.name, variable.value]));
}

function substituteVariables(query: string, variables: Record<string, string>) {
  return query.replace(/\$([a-zA-Z_][a-zA-Z0-9_]*)/g, (_match, name: string) => variables[name] ?? "");
}

function cleanVariableName(value: string) {
  const cleaned = value.replace(/^\$/, "").replace(/[^a-zA-Z0-9_]/g, "");
  return cleaned || "variable";
}
