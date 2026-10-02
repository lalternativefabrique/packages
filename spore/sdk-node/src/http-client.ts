export interface SporeClientOptions {
  /**
   * Base URL of the Spore API. Defaults to https://api.sporee.fr.
   * Override for staging or local dev (e.g. http://localhost:4110).
   */
  baseURL?: string;

  /**
   * Bearer token: a managed `sk_live_*` API key, an HS256 JWT, or the static
   * API_KEY value. Sent as `Authorization: Bearer <token>`.
   */
  apiKey?: string;

  /** Forwarded to every `fetch` call; use `"include"` to send cookies from a browser. */
  credentials?: RequestCredentials;

  /** Replaces the global `fetch`, to add retries, telemetry or a test double. */
  fetch?: typeof fetch;
}

export interface SporeRequestConfig {
  url: string;
  method: string;
  headers?: Record<string, string>;
  params?: object;
  data?: unknown;
}

export interface SporeRequestOptions {
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export class SporeError extends Error {
  readonly status: number;
  readonly body: unknown;
  readonly response: Response;

  constructor(response: Response, body: unknown) {
    super(errorMessage(response, body));
    this.name = "SporeError";
    this.status = response.status;
    this.body = body;
    this.response = response;
  }
}

export function isSporeError(err: unknown): err is SporeError {
  return err instanceof SporeError;
}

const DEFAULT_BASE_URL = "https://api.sporee.fr";

let clientOptions: SporeClientOptions = {};

/** Configures every generated SDK call. Safe to re-call: it replaces the previous options. */
export function configureSporeClient(opts: SporeClientOptions): void {
  clientOptions = { ...opts };
}

export const sporeHttp = async <T>(
  config: SporeRequestConfig,
  options?: SporeRequestOptions,
): Promise<T> => {
  const { baseURL = DEFAULT_BASE_URL, apiKey, credentials } = clientOptions;
  const doFetch = clientOptions.fetch ?? fetch;

  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
    ...config.headers,
    ...options?.headers,
  };

  const response = await doFetch(buildURL(baseURL, config.url, config.params), {
    method: config.method,
    headers,
    body: encodeBody(config.data, headers["Content-Type"]),
    credentials,
    signal: options?.signal,
  });

  const body = await decodeBody(response);
  if (!response.ok) throw new SporeError(response, body);
  return body as T;
};

function buildURL(baseURL: string, path: string, params?: object): string {
  const url = `${baseURL.replace(/\/+$/, "")}${path}`;
  if (!params) return url;
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    for (const item of Array.isArray(value) ? value : [value]) {
      query.append(key, String(item));
    }
  }
  const qs = query.toString();
  return qs ? `${url}?${qs}` : url;
}

function encodeBody(data: unknown, contentType?: string): BodyInit | undefined {
  if (data === undefined) return undefined;
  if (typeof data === "string" && contentType !== "application/json") return data;
  return JSON.stringify(data);
}

async function decodeBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return undefined;
  if (!response.headers.get("Content-Type")?.includes("json")) return text;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function errorMessage(response: Response, body: unknown): string {
  if (body && typeof body === "object" && "message" in body) {
    const message = (body as { message: unknown }).message;
    if (typeof message === "string" && message) return message;
  }
  return `Spore API ${response.status} ${response.statusText}`.trim();
}

export default sporeHttp;
