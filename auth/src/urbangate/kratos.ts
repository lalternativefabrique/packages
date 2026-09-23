export type Fetch = typeof fetch;

export interface KratosFlow {
  id: string;
  ui?: {
    messages?: Array<KratosMessage>;
    nodes?: Array<{
      attributes?: { name?: string; value?: unknown };
      messages?: Array<KratosMessage>;
    }>;
  };
  state?: string;
}

export interface KratosMessage {
  id?: number;
  text?: string;
  type?: string;
}

export interface KratosSession {
  id: string;
  active?: boolean;
  expires_at?: string;
  authenticated_at?: string;
  identity?: {
    id: string;
    state?: string;
    traits?: { email?: string; name?: string };
    verifiable_addresses?: Array<{ value: string; verified: boolean }>;
  };
}

export interface KratosSessionResult {
  session_token?: string;
  session?: KratosSession;
  continue_with?: Array<ContinueWith>;
}

export type ContinueWith =
  | { action: "show_verification_ui"; flow: { id: string } }
  | { action: "show_settings_ui"; flow: { id: string } }
  | { action: "set_ory_session_token"; ory_session_token: string }
  | { action: string };

export type KratosFailure =
  | { status: "invalid_credentials" }
  | { status: "invalid_code" }
  | { status: "email_not_verified" }
  | { status: "second_factor_required" }
  | { status: "account_disabled" }
  | { status: "already_registered" }
  | { status: "account_not_found" }
  | { status: "password_refused"; message: string }
  | { status: "invalid_input"; message: string }
  | { status: "flow_expired" }
  | { status: "unavailable" };

export class KratosError extends Error {
  failure: KratosFailure;
  constructor(failure: KratosFailure) {
    super(failure.status);
    this.failure = failure;
  }
}

// Kratos' message ids, from its text catalogue. A wrong password and an
// unknown address share one id on purpose.
const INVALID_CREDENTIALS = 4000006;
const INVALID_CODE = new Set([4000008, 4000016, 4010008, 4060006, 4070006]);
const CODE_SENT = new Set([1010014, 1040005, 1060003, 1070003, 1080003]);
const ALREADY_REGISTERED = 4000007;
const ACCOUNT_NOT_FOUND = 4000035;
const PASSWORD_POLICY = new Set([4000005, 4000031, 4000032, 4000033, 4000034]);
const FLOW_EXPIRED = new Set([4010001, 4040001, 4060005, 4070005]);

export function failureOf(status: number, body: unknown): KratosFailure {
  const b = (body ?? {}) as {
    error?: { id?: string; code?: number; message?: string };
    ui?: KratosFlow["ui"];
  };
  if (status === 410 || FLOW_EXPIRED.has(b.error?.code ?? -1)) {
    return { status: "flow_expired" };
  }
  if (b.error?.id === "session_aal2_required") {
    return { status: "second_factor_required" };
  }
  if (b.error?.id === "self_service_flow_expired")
    return { status: "flow_expired" };
  if (status >= 500 || status === 0) return { status: "unavailable" };
  const messages = [
    ...(b.ui?.messages ?? []),
    ...(b.ui?.nodes ?? []).flatMap((n) => n.messages ?? []),
  ].filter((m) => m.type === "error");
  for (const m of messages) {
    if (m.id === INVALID_CREDENTIALS) return { status: "invalid_credentials" };
    if (INVALID_CODE.has(m.id ?? -1)) return { status: "invalid_code" };
    if (m.id === ALREADY_REGISTERED) return { status: "already_registered" };
    if (m.id === ACCOUNT_NOT_FOUND) return { status: "account_not_found" };
    if (PASSWORD_POLICY.has(m.id ?? -1)) {
      return { status: "password_refused", message: m.text ?? "" };
    }
    if (FLOW_EXPIRED.has(m.id ?? -1)) return { status: "flow_expired" };
  }
  if (b.error?.id === "account_disabled" || status === 401) {
    return { status: "account_disabled" };
  }
  const first = messages[0]?.text ?? b.error?.message ?? "";
  return { status: "invalid_input", message: first };
}

export function codeWasSent(flow: KratosFlow): boolean {
  return (flow.ui?.messages ?? []).some((m) => CODE_SENT.has(m.id ?? -1));
}

export function csrfOf(flow: KratosFlow): string | undefined {
  const node = flow.ui?.nodes?.find((n) => n.attributes?.name === "csrf_token");
  return typeof node?.attributes?.value === "string"
    ? node.attributes.value
    : undefined;
}

export type FlowKind =
  "login" | "registration" | "verification" | "recovery" | "settings";

/**
 * Kratos' native (API) flows, the ones an app that renders its own screens
 * drives from its server: no cookies, no CSRF, a session token in the answer.
 */
export class KratosFlows {
  private readonly baseUrl: string;
  private readonly fetchImpl: Fetch;

  constructor(baseUrl: string, fetchImpl: Fetch = fetch) {
    this.baseUrl = baseUrl;
    this.fetchImpl = fetchImpl;
  }

  async start(
    kind: FlowKind,
    sessionToken?: string,
    params: Record<string, string> = {},
  ): Promise<KratosFlow> {
    const url = new URL(`/self-service/${kind}/api`, this.baseUrl);
    for (const [k, v] of Object.entries(params)) url.searchParams.set(k, v);
    const res = await this.call(url, { method: "GET" }, sessionToken);
    return (await this.body(res)) as KratosFlow;
  }

  async submit<T = KratosFlow>(
    kind: FlowKind,
    flowId: string,
    body: Record<string, unknown>,
    sessionToken?: string,
  ): Promise<T> {
    const url = new URL(`/self-service/${kind}`, this.baseUrl);
    url.searchParams.set("flow", flowId);
    const res = await this.call(
      url,
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      },
      sessionToken,
    );
    return (await this.body(res)) as T;
  }

  async whoami(sessionToken: string): Promise<KratosSession | null> {
    const url = new URL("/sessions/whoami", this.baseUrl);
    let res: Response;
    try {
      res = await this.fetchImpl(url, {
        headers: { "x-session-token": sessionToken },
      });
    } catch {
      throw new KratosError({ status: "unavailable" });
    }
    if (res.status === 401 || res.status === 403) return null;
    if (!res.ok) throw new KratosError({ status: "unavailable" });
    return (await res.json()) as KratosSession;
  }

  async logout(sessionToken: string): Promise<void> {
    const url = new URL("/self-service/logout/api", this.baseUrl);
    try {
      await this.fetchImpl(url, {
        method: "DELETE",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ session_token: sessionToken }),
      });
    } catch {
      // The cookie is dropped regardless; a token Kratos still knows expires
      // on its own, and the person is signed out of this app either way.
    }
  }

  private async call(
    url: URL,
    init: RequestInit,
    sessionToken?: string,
  ): Promise<Response> {
    const headers = new Headers(init.headers);
    headers.set("accept", "application/json");
    if (sessionToken) headers.set("x-session-token", sessionToken);
    try {
      return await this.fetchImpl(url, { ...init, headers });
    } catch {
      throw new KratosError({ status: "unavailable" });
    }
  }

  private async body(res: Response): Promise<unknown> {
    const body = await res.json().catch(() => null);
    // A recovery code that verified answers 422 with what to do next, which
    // is a success shaped as a refusal.
    if (
      res.ok ||
      (res.status === 422 && body && "continue_with" in (body as object))
    ) {
      return body;
    }
    // A submitted flow that still has errors comes back as the flow itself
    // with a 400, and the messages say which field.
    throw new KratosError(failureOf(res.status, body));
  }
}
