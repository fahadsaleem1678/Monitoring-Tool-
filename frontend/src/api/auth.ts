export type AuthUser = {
  id: string;
  username: string;
  role: "admin" | "viewer";
};

export type AuthSession = {
  token: string;
  user: AuthUser;
};

export async function login(username: string, password: string): Promise<AuthSession> {
  const response = await fetch("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password })
  });
  return readJSON<AuthSession>(response);
}

export async function me(token: string): Promise<{ user: AuthUser }> {
  const response = await fetch("/api/v1/auth/me", {
    headers: authHeaders(token)
  });
  return readJSON<{ user: AuthUser }>(response);
}

export function authHeaders(token: string) {
  return { Authorization: `Bearer ${token}` };
}

async function readJSON<T>(response: Response): Promise<T> {
  const body = (await response.json()) as T | { error?: string };
  if (!response.ok) {
    const message = typeof body === "object" && body !== null && "error" in body ? body.error : "";
    throw new Error(message || `HTTP ${response.status}`);
  }
  return body as T;
}
