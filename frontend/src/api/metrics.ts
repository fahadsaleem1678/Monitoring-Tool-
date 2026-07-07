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

type PrometheusScalarData = {
  resultType: "scalar";
  result: [number, string];
};

type ScalarMetricsResponse = {
  data: PrometheusScalarData;
};

type LabelValuesResponse = {
  data: string[];
};

let prometheusTimeCache: { value: number; expiresAt: number } | null = null;

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
  const end = await prometheusNowSeconds();
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

export async function listMetricNames(): Promise<string[]> {
  const params = new URLSearchParams({ label: "__name__" });
  const response = await fetch(`/api/v1/metrics/label-values?${params.toString()}`);
  const body = (await response.json()) as LabelValuesResponse | { error?: string };
  if (!response.ok) {
    throw new Error("error" in body && body.error ? body.error : `Metric names request failed with HTTP ${response.status}`);
  }
  if (!("data" in body) || !Array.isArray(body.data)) {
    throw new Error("Metric names response did not include data");
  }
  return body.data;
}

export async function listLabelValues(label: string): Promise<string[]> {
  const params = new URLSearchParams({ label });
  const response = await fetch(`/api/v1/metrics/label-values?${params.toString()}`);
  const body = (await response.json()) as LabelValuesResponse | { error?: string };
  if (!response.ok) {
    throw new Error("error" in body && body.error ? body.error : `Label values request failed with HTTP ${response.status}`);
  }
  if (!("data" in body) || !Array.isArray(body.data)) {
    throw new Error("Label values response did not include data");
  }
  return body.data;
}

async function prometheusNowSeconds(): Promise<number> {
  const now = Date.now();
  if (prometheusTimeCache && prometheusTimeCache.expiresAt > now) {
    return prometheusTimeCache.value;
  }

  const params = new URLSearchParams({ query: "time()" });
  const response = await fetch(`/api/v1/metrics/query?${params.toString()}`);
  const body = (await response.json()) as ScalarMetricsResponse | { error?: string };
  if (!response.ok) {
    throw new Error("error" in body && body.error ? body.error : `Prometheus time request failed with HTTP ${response.status}`);
  }
  if (!("data" in body) || body.data.resultType !== "scalar") {
    throw new Error("Prometheus time response was not a scalar");
  }

  const value = Number(body.data.result[1]);
  if (!Number.isFinite(value)) {
    throw new Error("Prometheus time response was invalid");
  }

  prometheusTimeCache = { value, expiresAt: now + 15_000 };
  return value;
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
