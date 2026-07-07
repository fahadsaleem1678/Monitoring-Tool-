import type { PrometheusQueryData, PrometheusVectorResult } from "./metrics";

export type LivePanelSubscription = {
  panel_id: string;
  promql: string;
  refresh_interval_seconds: number;
};

export type LiveMessage =
  | {
      type: "metric_update";
      panel_id: string;
      timestamp: number;
      data: PrometheusQueryData<PrometheusVectorResult>;
    }
  | {
      type: "error";
      panel_id?: string;
      message: string;
    }
  | {
      type: "subscribed" | "unsubscribed";
    };

type LiveCallbacks = {
  onMessage: (message: LiveMessage) => void;
  onDisconnect: () => void;
};

export function connectLivePanels(token: string, panels: LivePanelSubscription[], callbacks: LiveCallbacks) {
  const socket = new WebSocket(liveURL(token));

  socket.addEventListener("open", () => {
    socket.send(JSON.stringify({ type: "subscribe", panels }));
  });

  socket.addEventListener("message", (event) => {
    try {
      callbacks.onMessage(JSON.parse(event.data) as LiveMessage);
    } catch {
      callbacks.onMessage({ type: "error", message: "Live update message could not be parsed" });
    }
  });

  socket.addEventListener("close", callbacks.onDisconnect);
  socket.addEventListener("error", callbacks.onDisconnect);

  return () => {
    socket.removeEventListener("close", callbacks.onDisconnect);
    socket.removeEventListener("error", callbacks.onDisconnect);
    if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
      socket.close();
    }
  };
}

function liveURL(token: string) {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const params = new URLSearchParams({ token });
  return `${protocol}//${window.location.host}/ws/live?${params.toString()}`;
}
