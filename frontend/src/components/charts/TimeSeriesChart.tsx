import { useEffect, useMemo, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

type Series = {
  label: string;
  points: Array<[number, number]>;
};

type TimeSeriesChartProps = {
  series: Series[];
  unit?: string;
};

export function TimeSeriesChart({ series, unit = "" }: TimeSeriesChartProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const plotRef = useRef<uPlot | null>(null);

  const data = useMemo(() => toUPlotData(series), [series]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    const buildOptions = (): uPlot.Options => ({
      width: Math.max(container.clientWidth, 320),
      height: 220,
      padding: [8, 8, 0, 0],
      cursor: { drag: { x: true, y: false } },
      scales: { x: { time: true } },
      axes: [
        {
          stroke: "#8ea2b8",
          grid: { stroke: "#2b333d", width: 1 }
        },
        {
          stroke: "#8ea2b8",
          grid: { stroke: "#2b333d", width: 1 },
          values: (_u, values) => values.map((value) => formatNumber(value, unit))
        }
      ],
      series: [
        {},
        ...series.map((item, index) => ({
          label: item.label,
          stroke: chartColors[index % chartColors.length],
          width: 2
        }))
      ]
    });

    plotRef.current?.destroy();
    plotRef.current = new uPlot(buildOptions(), data, container);

    const resize = () => {
      plotRef.current?.setSize({
        width: Math.max(container.clientWidth, 320),
        height: 220
      });
    };
    window.addEventListener("resize", resize);

    return () => {
      window.removeEventListener("resize", resize);
      plotRef.current?.destroy();
      plotRef.current = null;
    };
  }, [data, series, unit]);

  if (series.length === 0) {
    return <div className="chart-empty">No data</div>;
  }

  return <div className="chart-frame" ref={containerRef} />;
}

function toUPlotData(series: Series[]): uPlot.AlignedData {
  const timestamps = Array.from(new Set(series.flatMap((item) => item.points.map(([timestamp]) => timestamp)))).sort(
    (a, b) => a - b
  );

  return [
    timestamps,
    ...series.map((item) => {
      const valueByTime = new Map(item.points.map(([timestamp, value]) => [timestamp, value]));
      return timestamps.map((timestamp) => valueByTime.get(timestamp) ?? null);
    })
  ];
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

const chartColors = ["#70d6ff", "#ffca5f", "#72d79b", "#f17d73", "#c9a7ff", "#f4a261"];
