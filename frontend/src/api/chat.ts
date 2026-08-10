import { authHeaders } from "./auth";

export type ChatFact = {
  label: string;
  value: string;
  severity: "healthy" | "warning" | "error" | string;
};

export type ChatResponse = {
  answer: string;
  intent: string;
  confidence: "low" | "medium" | "high" | string;
  facts: ChatFact[];
  queries: string[];
  suggestions: string[];
};

export type ChatContext = {
  pods: string[];
  last_intent: string;
};

export async function askClusterAssistant(token: string, message: string, context?: ChatContext): Promise<ChatResponse> {
  const response = await fetch("/api/v1/chat/query", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ message, context })
  });
  return readJSON<ChatResponse>(response);
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json()) as T | { error?: string };
  if (!response.ok) {
    const message = typeof body === "object" && body !== null && "error" in body ? body.error : "";
    throw new Error(message || `HTTP ${response.status}`);
  }
  return body as T;
}
