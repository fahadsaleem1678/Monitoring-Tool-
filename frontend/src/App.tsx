import { useEffect, useState } from "react";
import { getBackendHealth, type BackendHealth } from "./api/health";
import { StatusBadge } from "./components/StatusBadge";

type LoadState =
  | { status: "loading" }
  | { status: "healthy"; data: BackendHealth }
  | { status: "error"; message: string };

export function App() {
  const [state, setState] = useState<LoadState>({ status: "loading" });

  useEffect(() => {
    let active = true;

    getBackendHealth()
      .then((data) => {
        if (active) {
          setState({ status: "healthy", data });
        }
      })
      .catch((error: unknown) => {
        if (active) {
          setState({
            status: "error",
            message: error instanceof Error ? error.message : "Unknown error"
          });
        }
      });

    return () => {
      active = false;
    };
  }, []);

  return (
    <main className="app-shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Phase 0</p>
          <h1>Kubernetes Monitoring</h1>
        </div>
        <StatusBadge status={state.status} />
      </section>

      <section className="panel">
        <h2>Backend Health</h2>
        {state.status === "loading" && <p>Checking backend...</p>}
        {state.status === "error" && <p className="error">{state.message}</p>}
        {state.status === "healthy" && (
          <dl className="health-grid">
            <div>
              <dt>Status</dt>
              <dd>{state.data.status}</dd>
            </div>
            <div>
              <dt>Service</dt>
              <dd>{state.data.service}</dd>
            </div>
            <div>
              <dt>Started</dt>
              <dd>{new Date(state.data.started_at).toLocaleString()}</dd>
            </div>
          </dl>
        )}
      </section>
    </main>
  );
}
