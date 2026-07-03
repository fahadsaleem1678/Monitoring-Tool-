import { TimeSeriesChart } from "../charts/TimeSeriesChart";

type PanelSeries = {
  label: string;
  points: Array<[number, number]>;
};

type PanelProps = {
  title: string;
  subtitle: string;
  status: "loading" | "ready" | "error";
  error?: string;
  series: PanelSeries[];
  unit?: string;
};

export function Panel({ title, subtitle, status, error, series, unit }: PanelProps) {
  return (
    <section className="dashboard-panel">
      <header>
        <div>
          <h2>{title}</h2>
          <p>{subtitle}</p>
        </div>
        <span className={`panel-status ${status}`}>{status === "ready" ? "Live" : status}</span>
      </header>
      {status === "loading" && <div className="panel-message">Loading data...</div>}
      {status === "error" && <div className="panel-message error">{error}</div>}
      {status === "ready" && <TimeSeriesChart series={series} unit={unit} />}
    </section>
  );
}
