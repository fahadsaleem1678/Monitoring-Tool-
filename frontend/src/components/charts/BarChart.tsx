type Series = {
  label: string;
  points: Array<[number, number]>;
};

type BarChartProps = {
  series: Series[];
  unit?: string;
};

export function BarChart({ series, unit = "" }: BarChartProps) {
  const values = series.map((item) => ({
    label: item.label,
    value: lastValue(item.points)
  }));
  const max = Math.max(...values.map((item) => Math.abs(item.value)), 1);

  if (series.length === 0) {
    return <div className="chart-empty">No data</div>;
  }

  return (
    <div className="bar-chart" role="list">
      {values.map((item, index) => (
        <div className="bar-row" role="listitem" key={`${item.label}-${index}`}>
          <span className="bar-label">{item.label}</span>
          <div className="bar-track">
            <span className="bar-fill" style={{ width: `${Math.min((Math.abs(item.value) / max) * 100, 100)}%` }} />
          </div>
          <strong>{formatNumber(item.value, unit)}</strong>
        </div>
      ))}
    </div>
  );
}

function lastValue(points: Array<[number, number]>) {
  return points.length > 0 ? points[points.length - 1][1] : 0;
}

function formatNumber(value: number, unit: string) {
  if (unit === "%") {
    return `${value.toFixed(0)}%`;
  }
  if (Math.abs(value) >= 1000) {
    return value.toFixed(0);
  }
  if (Math.abs(value) >= 10) {
    return value.toFixed(1);
  }
  return value.toFixed(2);
}
