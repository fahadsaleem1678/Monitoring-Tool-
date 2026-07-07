import { useEffect, useMemo, useState } from "react";
import { listLabelValues, listMetricNames } from "../../api/metrics";

type VisualQueryBuilderProps = {
  value: string;
  onApply: (query: string) => void;
};

type QueryFilter = {
  id: number;
  label: string;
  operator: "=" | "!=" | "=~" | "!~";
  value: string;
};

type BuilderState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready" }
  | { status: "error"; message: string };

const aggregations = ["none", "sum", "avg", "min", "max", "count"] as const;

const commonLabels = ["job", "namespace", "pod", "service", "instance", "container", "node", "status", "method"];

export function VisualQueryBuilder({ value, onApply }: VisualQueryBuilderProps) {
  const [state, setState] = useState<BuilderState>({ status: "idle" });
  const [metrics, setMetrics] = useState<string[]>([]);
  const [metricSearch, setMetricSearch] = useState("");
  const [metric, setMetric] = useState(metricFromQuery(value) || "up");
  const [filters, setFilters] = useState<QueryFilter[]>([]);
  const [aggregation, setAggregation] = useState<(typeof aggregations)[number]>("none");
  const [groupBy, setGroupBy] = useState("job");
  const [rateWindow, setRateWindow] = useState("");
  const [labelValueOptions, setLabelValueOptions] = useState<Record<string, string[]>>({});

  useEffect(() => {
    let closed = false;
    setState({ status: "loading" });
    listMetricNames()
      .then((items) => {
        if (closed) {
          return;
        }
        setMetrics(items);
        if (!metric && items.length > 0) {
          setMetric(items[0]);
        }
        setState({ status: "ready" });
      })
      .catch((error) => {
        if (!closed) {
          setState({ status: "error", message: error instanceof Error ? error.message : "Metric names failed" });
        }
      });
    return () => {
      closed = true;
    };
  }, []);

  const suggestedMetrics = useMemo(() => {
    const search = metricSearch.trim().toLowerCase();
    const source = search === "" ? metrics : metrics.filter((item) => item.toLowerCase().includes(search));
    return source.slice(0, 80);
  }, [metricSearch, metrics]);

  const query = buildQuery({ metric, filters, aggregation, groupBy, rateWindow });

  function addFilter() {
    setFilters((current) => [...current, { id: Date.now(), label: "job", operator: "=", value: "" }]);
  }

  function updateFilter(id: number, patch: Partial<QueryFilter>) {
    setFilters((current) => current.map((filter) => (filter.id === id ? { ...filter, ...patch } : filter)));
  }

  function removeFilter(id: number) {
    setFilters((current) => current.filter((filter) => filter.id !== id));
  }

  function loadValues(label: string) {
    if (labelValueOptions[label]) {
      return;
    }
    void listLabelValues(label)
      .then((values) => setLabelValueOptions((current) => ({ ...current, [label]: values.slice(0, 120) })))
      .catch(() => setLabelValueOptions((current) => ({ ...current, [label]: [] })));
  }

  return (
    <section className="visual-builder">
      <header>
        <div>
          <h3>Visual Query Builder</h3>
          <p>Select a metric, filters, and aggregation to generate PromQL.</p>
        </div>
        {state.status === "loading" && <span>Loading metrics...</span>}
        {state.status === "error" && <span className="builder-error">{state.message}</span>}
      </header>

      <div className="builder-grid">
        <label>
          Metric
          <input
            list="metric-options"
            value={metricSearch || metric}
            onChange={(event) => {
              setMetricSearch(event.target.value);
              setMetric(event.target.value);
            }}
            onBlur={() => setMetricSearch("")}
            placeholder="up"
            spellCheck={false}
          />
          <datalist id="metric-options">
            {suggestedMetrics.map((item) => (
              <option key={item} value={item} />
            ))}
          </datalist>
        </label>

        <label>
          Aggregation
          <select value={aggregation} onChange={(event) => setAggregation(event.target.value as typeof aggregation)}>
            {aggregations.map((item) => (
              <option key={item} value={item}>
                {item === "none" ? "None" : item}
              </option>
            ))}
          </select>
        </label>

        <label>
          Group by
          <input
            value={groupBy}
            onChange={(event) => setGroupBy(event.target.value)}
            placeholder="service, job, namespace"
            disabled={aggregation === "none"}
          />
        </label>

        <label>
          Rate window
          <input value={rateWindow} onChange={(event) => setRateWindow(event.target.value)} placeholder="5m for counters" />
        </label>
      </div>

      <div className="filter-stack">
        <div className="builder-section-head">
          <strong>Filters</strong>
          <button type="button" onClick={addFilter}>
            Add filter
          </button>
        </div>
        {filters.length === 0 && <div className="builder-empty">No filters</div>}
        {filters.map((filter) => (
          <div className="filter-row" key={filter.id}>
            <input
              list="label-options"
              value={filter.label}
              onFocus={() => loadValues(filter.label)}
              onChange={(event) => {
                updateFilter(filter.id, { label: event.target.value, value: "" });
                loadValues(event.target.value);
              }}
              placeholder="label"
            />
            <select value={filter.operator} onChange={(event) => updateFilter(filter.id, { operator: event.target.value as QueryFilter["operator"] })}>
              <option value="=">=</option>
              <option value="!=">!=</option>
              <option value="=~">=~</option>
              <option value="!~">!~</option>
            </select>
            <input
              list={`label-values-${filter.id}`}
              value={filter.value}
              onFocus={() => loadValues(filter.label)}
              onChange={(event) => updateFilter(filter.id, { value: event.target.value })}
              placeholder="value"
            />
            <datalist id={`label-values-${filter.id}`}>
              {(labelValueOptions[filter.label] ?? []).map((item) => (
                <option key={item} value={item} />
              ))}
            </datalist>
            <button type="button" className="danger-button small" onClick={() => removeFilter(filter.id)}>
              Remove
            </button>
          </div>
        ))}
        <datalist id="label-options">
          {commonLabels.map((label) => (
            <option key={label} value={label} />
          ))}
        </datalist>
      </div>

      <div className="generated-query">
        <code>{query}</code>
        <button type="button" onClick={() => onApply(query)}>
          Use Query
        </button>
      </div>
    </section>
  );
}

function buildQuery({
  metric,
  filters,
  aggregation,
  groupBy,
  rateWindow
}: {
  metric: string;
  filters: QueryFilter[];
  aggregation: string;
  groupBy: string;
  rateWindow: string;
}) {
  const safeMetric = metric.trim() || "up";
  const selectors = filters
    .filter((filter) => filter.label.trim() !== "" && filter.value.trim() !== "")
    .map((filter) => `${filter.label.trim()}${filter.operator}"${escapeLabelValue(filter.value.trim())}"`);
  const selector = selectors.length > 0 ? `{${selectors.join(",")}}` : "";
  const base = rateWindow.trim() ? `rate(${safeMetric}${selector}[${rateWindow.trim()}])` : `${safeMetric}${selector}`;
  if (aggregation === "none") {
    return base;
  }
  const groups = groupBy
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
    .join(", ");
  return groups ? `${aggregation} by (${groups}) (${base})` : `${aggregation}(${base})`;
}

function escapeLabelValue(value: string) {
  return value.replace(/\\/g, "\\\\").replace(/"/g, "\\\"");
}

function metricFromQuery(query: string) {
  const match = query.match(/[a-zA-Z_:][a-zA-Z0-9_:]*/);
  return match?.[0] ?? "";
}
