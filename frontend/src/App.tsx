import { useEffect, useMemo, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { login, me, type AuthSession } from "./api/auth";
import { getBackendHealth, type BackendHealth } from "./api/health";
import { queryInstant, queryRange, type PrometheusMatrixResult, type PrometheusVectorResult } from "./api/metrics";
import { AlertsView } from "./components/alerts/AlertsView";
import { LoginView } from "./components/auth/LoginView";
import { DashboardManager } from "./components/dashboards/DashboardManager";
import { MetricCard } from "./components/overview/MetricCard";
import { Panel } from "./components/overview/Panel";
import { QueryWorkbench } from "./components/overview/QueryWorkbench";
import { StatusBadge } from "./components/StatusBadge";

type Loadable<T> =
  | { status: "loading" }
  | { status: "ready"; data: T }
  | { status: "error"; message: string };

type PanelDefinition = {
  id: string;
  title: string;
  subtitle: string;
  query: string;
  unit?: string;
  transform?: (value: number) => number;
};

const panels: PanelDefinition[] = [
  {
    id: "cpu",
    title: "Cluster CPU Usage",
    subtitle: "Average active CPU by node",
    query: '100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)',
    unit: "%"
  },
  {
    id: "memory",
    title: "Cluster Memory Usage",
    subtitle: "Memory used by node",
    query: "(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100",
    unit: "%"
  },
  {
    id: "restarts",
    title: "Pod Restarts",
    subtitle: "Restart rate by namespace",
    query: "sum by (namespace) (increase(kube_pod_container_status_restarts_total[5m]))"
  }
];

export function App() {
  const [session, setSession] = useState<AuthSession | null>(() => readStoredSession());
  const [sessionChecked, setSessionChecked] = useState(false);
  const [activeView, setActiveView] = useState<"overview" | "dashboards" | "alerts">("overview");
  const [health, setHealth] = useState<Loadable<BackendHealth>>({ status: "loading" });
  const [cards, setCards] = useState<Loadable<CardMetrics>>({ status: "loading" });
  const [panelData, setPanelData] = useState<Record<string, Loadable<PrometheusMatrixResult[]>>>(
    Object.fromEntries(panels.map((panel) => [panel.id, { status: "loading" }]))
  );

  useEffect(() => {
    if (!session) {
      setSessionChecked(true);
      return;
    }
    me(session.token)
      .then(({ user }) => {
        const fresh = { ...session, user };
        storeSession(fresh);
        setSession(fresh);
      })
      .catch(() => {
        clearSession();
        setSession(null);
      })
      .finally(() => setSessionChecked(true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!session) {
      return;
    }
    const refresh = () => {
      void loadHealth(setHealth);
      void loadCards(setCards);
      for (const panel of panels) {
        void loadPanel(panel, setPanelData);
      }
    };

    refresh();
    const interval = window.setInterval(refresh, 30000);
    return () => window.clearInterval(interval);
  }, [session]);

  const lastUpdated = useMemo(() => new Date().toLocaleTimeString(), [cards, panelData]);

  async function handleLogin(username: string, password: string) {
    const nextSession = await login(username, password);
    storeSession(nextSession);
    setSession(nextSession);
    setSessionChecked(true);
  }

  function handleLogout() {
    clearSession();
    setSession(null);
  }

  if (!sessionChecked) {
    return <main className="app-shell">Loading session...</main>;
  }

  if (!session) {
    return <LoginView onLogin={handleLogin} />;
  }

  return (
    <main className="app-shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">K3s cluster</p>
          <h1>Monitoring Tool</h1>
        </div>
        <div className="topbar-actions">
          <StatusBadge status={health.status === "ready" ? "healthy" : health.status} />
          <span>{session.user.username} · {session.user.role}</span>
          <button type="button" onClick={handleLogout}>
            Logout
          </button>
        </div>
      </section>

      <nav className="app-tabs" aria-label="Main views">
        <button type="button" className={activeView === "overview" ? "active" : ""} onClick={() => setActiveView("overview")}>
          Overview
        </button>
        <button type="button" className={activeView === "dashboards" ? "active" : ""} onClick={() => setActiveView("dashboards")}>
          Dashboards
        </button>
        <button type="button" className={activeView === "alerts" ? "active" : ""} onClick={() => setActiveView("alerts")}>
          Alerts
        </button>
      </nav>

      {activeView === "overview" && (
        <>
          <section className="summary-strip">
            {cards.status === "loading" && <MetricCard title="Cluster" value="Loading" detail="Fetching Prometheus" />}
            {cards.status === "error" && <MetricCard title="Cluster" value="Error" detail={cards.message} state="error" />}
            {cards.status === "ready" && (
              <>
                <MetricCard title="Nodes Ready" value={cards.data.nodesReady} detail="kube-state-metrics" />
                <MetricCard title="Pods" value={cards.data.pods} detail="Known running pods" />
                <MetricCard title="Targets Up" value={cards.data.targetsUp} detail="Prometheus scrape health" />
              </>
            )}
          </section>

          <section className="dashboard-grid">
            {panels.map((panel) => {
              const state = panelData[panel.id] ?? { status: "loading" as const };
              return (
                <Panel
                  key={panel.id}
                  title={panel.title}
                  subtitle={panel.subtitle}
                  status={state.status}
                  error={state.status === "error" ? state.message : undefined}
                  series={state.status === "ready" ? matrixToSeries(state.data, panel.transform) : []}
                  unit={panel.unit}
                />
              );
            })}
          </section>

          <section className="workbench-band">
            <header>
              <div>
                <h2>PromQL Query</h2>
                <p>Instant query preview through the Go backend</p>
              </div>
              <span>Last refresh {lastUpdated}</span>
            </header>
            <QueryWorkbench />
          </section>
        </>
      )}

      {activeView === "dashboards" && <DashboardManager token={session.token} user={session.user} />}
      {activeView === "alerts" && <AlertsView token={session.token} user={session.user} />}
    </main>
  );
}

type CardMetrics = {
  nodesReady: string;
  pods: string;
  targetsUp: string;
};

async function loadHealth(setHealth: (state: Loadable<BackendHealth>) => void) {
  try {
    setHealth({ status: "ready", data: await getBackendHealth() });
  } catch (error) {
    setHealth({ status: "error", message: error instanceof Error ? error.message : "Backend health failed" });
  }
}

async function loadCards(setCards: (state: Loadable<CardMetrics>) => void) {
  try {
    const [nodes, pods, targets] = await Promise.all([
      queryInstant('sum(kube_node_status_condition{condition="Ready",status="true"})'),
      queryInstant('count(kube_pod_info)'),
      queryInstant("sum(up)")
    ]);
    setCards({
      status: "ready",
      data: {
        nodesReady: firstVectorValue(nodes.result),
        pods: firstVectorValue(pods.result),
        targetsUp: firstVectorValue(targets.result)
      }
    });
  } catch (error) {
    setCards({ status: "error", message: error instanceof Error ? error.message : "Summary metrics failed" });
  }
}

async function loadPanel(
  panel: PanelDefinition,
  setPanelData: Dispatch<SetStateAction<Record<string, Loadable<PrometheusMatrixResult[]>>>>
) {
  try {
    const data = await queryRange(panel.query, 60 * 60, 60);
    setPanelData((current) => ({ ...current, [panel.id]: { status: "ready", data: data.result } }));
  } catch (error) {
    setPanelData((current) => ({
      ...current,
      [panel.id]: { status: "error", message: error instanceof Error ? error.message : "Panel query failed" }
    }));
  }
}

function firstVectorValue(results: PrometheusVectorResult[]) {
  if (results.length === 0) {
    return "0";
  }
  const value = Number(results[0].value[1]);
  return Number.isFinite(value) ? value.toFixed(0) : "0";
}

function matrixToSeries(results: PrometheusMatrixResult[], transform?: (value: number) => number) {
  return results.slice(0, 6).map((result, index) => ({
    label: labelFromMetric(result.metric, index),
    points: result.values.map(([timestamp, value]) => {
      const numeric = Number(value);
      return [timestamp, transform ? transform(numeric) : numeric] as [number, number];
    })
  }));
}

function labelFromMetric(metric: Record<string, string>, index: number) {
  return metric.node ?? metric.instance ?? metric.namespace ?? metric.pod ?? metric.container ?? `series ${index + 1}`;
}

function readStoredSession(): AuthSession | null {
  const raw = window.localStorage.getItem("monitoring_session");
  if (!raw) {
    return null;
  }
  try {
    return JSON.parse(raw) as AuthSession;
  } catch {
    return null;
  }
}

function storeSession(session: AuthSession) {
  window.localStorage.setItem("monitoring_session", JSON.stringify(session));
}

function clearSession() {
  window.localStorage.removeItem("monitoring_session");
}
