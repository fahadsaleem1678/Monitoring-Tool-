type Series = {
  label: string;
  points: Array<[number, number]>;
};

type GaugeChartProps = {
  series: Series[];
  unit?: string;
  max?: number;
};

export function GaugeChart({ series, unit = "", max }: GaugeChartProps) {
  if (series.length === 0) {
    return <div className="chart-empty">No data</div>;
  }

  const primary = series[0];
  const value = lastValue(primary.points);
  const ceiling = max && max > 0 ? max : unit === "%" ? 100 : Math.max(value, 1);
  const percent = Math.max(0, Math.min((value / ceiling) * 100, 100));
  const arcPercent = percent / 2;
  const rotation = -90 + (percent / 100) * 180;

  return (
    <div className="gauge-chart">
      <div className="gauge-arc" style={{ "--gauge-value": `${arcPercent}%` } as CSSProperties & Record<"--gauge-value", string>}>
        <span className="gauge-needle" style={{ transform: `rotate(${rotation}deg)` }} />
        <span className="gauge-hub" />
      </div>
      <strong>{formatNumber(value, unit)}</strong>
      <span>{primary.label}</span>
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
import type { CSSProperties } from "react";
