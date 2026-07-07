import { useState } from "react";
import { queryInstant, type PrometheusVectorResult } from "../../api/metrics";
import { VisualQueryBuilder } from "../query/VisualQueryBuilder";

type WorkbenchState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; results: PrometheusVectorResult[] }
  | { status: "error"; message: string };

export function QueryWorkbench() {
  const [query, setQuery] = useState("up");
  const [state, setState] = useState<WorkbenchState>({ status: "idle" });

  async function runQuery() {
    setState({ status: "loading" });
    try {
      const data = await queryInstant(query);
      setState({ status: "ready", results: data.result.slice(0, 8) });
    } catch (error) {
      setState({ status: "error", message: error instanceof Error ? error.message : "Query failed" });
    }
  }

  return (
    <section className="query-workbench">
      <VisualQueryBuilder value={query} onApply={setQuery} />
      <div className="query-row">
        <textarea value={query} onChange={(event) => setQuery(event.target.value)} spellCheck={false} />
        <button type="button" onClick={runQuery} disabled={state.status === "loading"}>
          Run
        </button>
      </div>
      {state.status === "loading" && <p>Running query...</p>}
      {state.status === "error" && <p className="error">{state.message}</p>}
      {state.status === "ready" && (
        <pre>{JSON.stringify(state.results, null, 2)}</pre>
      )}
    </section>
  );
}
