import type { AppKey, CreatedAppKey, KeysError, KeysTransport } from "./types";

export class KeysRequestError extends Error {
  readonly error: KeysError;

  constructor(error: KeysError) {
    super(error.kind);
    this.name = "KeysRequestError";
    this.error = error;
  }
}

function errorOf(status: number, body: unknown): KeysError {
  const reason =
    body &&
    typeof body === "object" &&
    typeof (body as { error?: unknown }).error === "string"
      ? (body as { error: string }).error
      : "";
  if (status === 401) return { kind: "unauthorized" };
  if (status === 403) return { kind: "forbidden" };
  if (status === 422) return { kind: "invalid", field: reason || "label" };
  return { kind: "unavailable" };
}

async function send(url: string, init: RequestInit): Promise<unknown> {
  let response: Response;
  try {
    response = await fetch(url, {
      ...init,
      headers: { "content-type": "application/json", ...init.headers },
    });
  } catch {
    throw new KeysRequestError({ kind: "unavailable" });
  }
  const text = await response.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      throw new KeysRequestError({ kind: "unavailable" });
    }
  }
  if (!response.ok) throw new KeysRequestError(errorOf(response.status, body));
  return body;
}

export function keyOf(raw: unknown): AppKey {
  const row = (raw ?? {}) as Record<string, unknown>;
  return {
    id: typeof row.id === "string" ? row.id : "",
    label: typeof row.label === "string" ? row.label : "",
    audience: Array.isArray(row.audience)
      ? row.audience.filter((a): a is string => typeof a === "string")
      : [],
    scopes: Array.isArray(row.scopes)
      ? row.scopes.filter((s): s is string => typeof s === "string")
      : [],
    createdAt: typeof row.createdAt === "string" ? row.createdAt : null,
    expiresAt: typeof row.expiresAt === "string" ? row.expiresAt : null,
  };
}

// The product's own route, never urbangate's: the machine credential that
// reaches the issuer stays on the product's server, so the browser holds a
// session and nothing more.
export function httpTransport(baseUrl: string): KeysTransport {
  const base = baseUrl.replace(/\/$/, "");
  return {
    list: async () => {
      const body = (await send(base, { method: "GET" })) as {
        keys?: Array<unknown>;
      } | null;
      return Array.isArray(body?.keys) ? body.keys.map(keyOf) : [];
    },
    create: async (input) => {
      const body = await send(base, {
        method: "POST",
        body: JSON.stringify(input),
      });
      const row = (body ?? {}) as Record<string, unknown>;
      const secret = typeof row.secret === "string" ? row.secret : "";
      if (!secret) throw new KeysRequestError({ kind: "unavailable" });
      return { ...keyOf(row), secret } as CreatedAppKey;
    },
    revoke: async (id) => {
      await send(`${base}/${encodeURIComponent(id)}`, { method: "DELETE" });
    },
  };
}
