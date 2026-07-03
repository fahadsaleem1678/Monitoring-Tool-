export type PrometheusVectorResult = {
  metric: Record<string, string>;
  value: [number, string];
};

export type PrometheusMatrixResult = {
  metric: Record<string, string>;
  values: Array<[number, string]>;
};

export type PrometheusQueryData<T> = {
  resultType: "vector" | "matrix" | "scalar" | "string";
  result: T[];
};

type MetricsResponse<T> = {
  data: PrometheusQueryData<T>;
};

export async function queryInstant(query: string): Promise<PrometheusQueryData<PrometheusVectorResult>> {
  const params = new URLSearchParams({ query });
  const response = await fetch(`/api/v1/metrics/query?${params.toString()}`);
  return readMetricsResponse<PrometheusVectorResult>(response);
}

export async function queryRange(
  query: string,
  rangeSeconds: number,
  stepSeconds: number
): Promise<PrometheusQueryData<PrometheusMatrixResult>> {
  const end = Date.now() / 1000;
  const start = end - rangeSeconds;
  const params = new URLSearchParams({
    query,
    start: start.toFixed(3),
    end: end.toFixed(3),
    step: String(stepSeconds)
  });
  const response = await fetch(`/api/v1/metrics/query-range?${params.toString()}`);
  return readMetricsResponse<PrometheusMatrixResult>(response);
}

async function readMetricsResponse<T>(response: Response): Promise<PrometheusQueryData<T>> {
  const body = (await response.json()) as MetricsResponse<T> | { error?: string };
  if (!response.ok) {
    throw new Error("error" in body && body.error ? body.error : `Metrics request failed with HTTP ${response.status}`);
  }
  if (!("data" in body)) {
    throw new Error("Metrics response did not include data");
  }
  return body.data;
}
