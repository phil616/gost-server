export type TunnelRuntime = {
  tunnel_id: string;
  state: string;
  error?: string;
  next_retry_at?: string;
  retry_count: number;
  input_bytes: number;
  output_bytes: number;
  updated_at: string;
};
export type Client = {
  id: string;
  name: string;
  os: string;
  arch: string;
  version: string;
  state: string;
  revision: number;
  received_revision: number;
  applied_revision: number;
  last_seen: string;
  revoked: boolean;
  runtime?: TunnelRuntime[];
};
export type Tunnel = {
  id: string;
  client_id: string;
  name: string;
  protocol: string;
  target_ip: string;
  target_port: number;
  enabled: boolean;
  generation: number;
  version: number;
  state: string;
  last_error?: string;
  next_retry_at?: string;
  active_connections: number;
};
export type Credential = {
  id: string;
  username: string;
  revoked: boolean;
  expires_at?: string;
};
export type Session = { username: string; csrf: string };
let csrf = "";
export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: "same-origin",
    ...options,
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": csrf,
      ...options.headers,
    },
  });
  const body = await response.json();
  if (!response.ok) {
    if (response.status === 401 && path !== "/auth/login")
      window.location.assign("/login");
    throw new Error(body.error || `HTTP ${response.status}`);
  }
  return body as T;
}
export async function me() {
  const session = await api<Session>("/auth/me");
  csrf = session.csrf;
  return session;
}
export async function login(username: string, password: string) {
  const session = await api<Session>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  csrf = session.csrf;
  return session;
}
export function mutation<T>(
  path: string,
  body: unknown = {},
  method = "POST",
  version?: number,
) {
  return api<T>(path, {
    method,
    body: JSON.stringify(body),
    headers: {
      "Idempotency-Key": crypto.randomUUID(),
      ...(version ? { "If-Match": String(version) } : {}),
    },
  });
}
