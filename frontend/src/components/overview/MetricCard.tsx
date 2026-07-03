type MetricCardProps = {
  title: string;
  value: string;
  detail?: string;
  state?: "ok" | "warn" | "error";
};

export function MetricCard({ title, value, detail, state = "ok" }: MetricCardProps) {
  return (
    <section className={`metric-card ${state}`}>
      <div>
        <h2>{title}</h2>
        {detail && <p>{detail}</p>}
      </div>
      <strong>{value}</strong>
    </section>
  );
}
