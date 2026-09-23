import { forwardHeaders } from "../core-proxy.ts";
import { clearCookie, readCookie, serializeCookie } from "./cookies.ts";
import { Exchange, decodeToken } from "./exchange.ts";
import type { ExchangeConfig, PersonToken } from "./exchange.ts";
import { KratosError, KratosFlows, codeWasSent } from "./kratos.ts";
import type {
  ContinueWith,
  Fetch,
  KratosFailure,
  KratosSession,
  KratosSessionResult,
} from "./kratos.ts";

export interface UrbangateAuthConfig {
  product: string;
  /** Kratos' public API as the server reaches it, e.g. http://kratos:4433 or https://id.urbangate.dev. */
  kratosUrl: string;
  urbangate: Omit<ExchangeConfig, "product">;
  cookie?: { name?: string; secure?: boolean };
  fetch?: Fetch;
}

export interface UrbangateUser {
  id: string;
  identityId: string;
  email: string;
  emailVerified: boolean;
  name: string;
  role: "admin" | "user";
}

export interface UrbangateSession {
  user: UrbangateUser;
  session: { expiresAt: string };
}

export interface AccessToken {
  token: string;
  setCookie?: string;
}

export interface UrbangateCoreProxyOptions {
  coreUrl: string;
  adminOnly?: boolean;
}

export interface UrbangateAuth {
  handler(request: Request): Promise<Response>;
  getSession(headers: Headers): Promise<UrbangateSession | null>;
  accessToken(headers: Headers): Promise<AccessToken | null>;
  coreProxy(
    options: UrbangateCoreProxyOptions,
  ): (request: Request) => Promise<Response>;
}

const SESSION_MAX_AGE = 30 * 24 * 60 * 60;
const FLOW_MAX_AGE = 600;
const TOKEN_MAX_AGE = 15 * 60;

type OtpType = "email-verification" | "forget-password" | "sign-in";

const FLOW_OF_TYPE: Record<OtpType, "verification" | "recovery" | "login"> = {
  "email-verification": "verification",
  "forget-password": "recovery",
  "sign-in": "login",
};

const FAILURE_STATUS: Record<KratosFailure["status"], number> = {
  invalid_credentials: 401,
  invalid_code: 400,
  email_not_verified: 403,
  second_factor_required: 403,
  account_disabled: 403,
  already_registered: 409,
  password_refused: 422,
  invalid_input: 400,
  flow_expired: 410,
  unavailable: 503,
};

function json(
  status: number,
  body: unknown,
  cookies: Array<string> = [],
): Response {
  const headers = new Headers({ "content-type": "application/json" });
  for (const c of cookies) headers.append("set-cookie", c);
  return new Response(JSON.stringify(body), { status, headers });
}

function failure(code: string, status: number, message?: string): Response {
  return json(status, {
    error: { code, status, ...(message ? { message } : {}) },
  });
}

function failed(error: unknown): Response {
  if (error instanceof KratosError) {
    const f = error.failure;
    return failure(
      f.status,
      FAILURE_STATUS[f.status],
      "message" in f ? f.message : undefined,
    );
  }
  return failure("unavailable", 503);
}

function userOf(
  session: KratosSession,
  roles: Array<string>,
  product: string,
): UrbangateUser | null {
  const identity = session.identity;
  if (!identity) return null;
  const email = identity.traits?.email ?? "";
  const address = identity.verifiable_addresses?.find((a) => a.value === email);
  return {
    id: identity.id,
    identityId: identity.id,
    email,
    emailVerified: address?.verified ?? false,
    name: identity.traits?.name ?? "",
    role: roles.includes(`${product}:admin`) ? "admin" : "user",
  };
}

function continueWith<A extends ContinueWith["action"]>(
  list: Array<ContinueWith> | undefined,
  action: A,
): Extract<ContinueWith, { action: A }> | undefined {
  return list?.find(
    (c): c is Extract<ContinueWith, { action: A }> => c.action === action,
  );
}

export function createUrbangateAuth(
  config: UrbangateAuthConfig,
): UrbangateAuth {
  const product = config.product;
  const kratos = new KratosFlows(config.kratosUrl, config.fetch);
  const exchange = new Exchange({ ...config.urbangate, product }, config.fetch);
  const names = {
    session: config.cookie?.name ?? `${product}_session`,
    token: `${product}_token`,
    flow: `${product}_flow`,
  };
  const secure =
    config.cookie?.secure ?? config.urbangate.issuerUrl.startsWith("https://");

  const cookie = (name: string, value: string, maxAge: number) =>
    serializeCookie(name, value, { maxAge, secure });

  const signedIn = (result: KratosSessionResult): Array<string> => {
    const token = result.session_token;
    if (!token) return [];
    return [
      cookie(names.session, token, SESSION_MAX_AGE),
      clearCookie(names.flow, secure),
    ];
  };

  const readToken = (headers: Headers): PersonToken | null => {
    const raw = readCookie(headers, names.token);
    const decoded = raw ? decodeToken(raw) : null;
    return raw && decoded ? { accessToken: raw, ...decoded } : null;
  };

  async function accessToken(headers: Headers): Promise<AccessToken | null> {
    const sessionToken = readCookie(headers, names.session);
    if (!sessionToken) return null;
    const held = readToken(headers);
    if (held && !exchange.needsRefresh(held))
      return { token: held.accessToken };
    const outcome = await exchange.exchange(sessionToken);
    if (outcome.status === "session_gone" || outcome.status === "inactive")
      return null;
    if (outcome.status === "unavailable")
      throw new KratosError({ status: "unavailable" });
    return {
      token: outcome.token.accessToken,
      setCookie: cookie(names.token, outcome.token.accessToken, TOKEN_MAX_AGE),
    };
  }

  async function getSession(
    headers: Headers,
  ): Promise<UrbangateSession | null> {
    const sessionToken = readCookie(headers, names.session);
    if (!sessionToken) return null;
    const session = await kratos.whoami(sessionToken);
    if (!session?.active) return null;
    const held = readToken(headers);
    const user = userOf(session, held?.roles ?? [], product);
    if (!user) return null;
    return { user, session: { expiresAt: session.expires_at ?? "" } };
  }

  async function body(request: Request): Promise<Record<string, unknown>> {
    const raw = (await request.json().catch(() => null)) as Record<
      string,
      unknown
    > | null;
    return raw ?? {};
  }

  const str = (b: Record<string, unknown>, key: string) =>
    typeof b[key] === "string" ? (b[key] as string).trim() : "";

  async function signInEmail(request: Request): Promise<Response> {
    const b = await body(request);
    const email = str(b, "email");
    const password = typeof b.password === "string" ? b.password : "";
    if (!email || !password)
      return failure("invalid_input", 400, "email and password are required");
    const flow = await kratos.start("login");
    const result = await kratos.submit<KratosSessionResult>("login", flow.id, {
      method: "password",
      identifier: email,
      password,
    });
    return json(
      200,
      { user: result.session ? userOf(result.session, [], product) : null },
      signedIn(result),
    );
  }

  async function signUpEmail(request: Request): Promise<Response> {
    const b = await body(request);
    const email = str(b, "email");
    const password = typeof b.password === "string" ? b.password : "";
    if (!email || !password)
      return failure("invalid_input", 400, "email and password are required");
    const flow = await kratos.start("registration");
    const result = await kratos.submit<KratosSessionResult>(
      "registration",
      flow.id,
      {
        method: "password",
        traits: { email, ...(str(b, "name") ? { name: str(b, "name") } : {}) },
        password,
      },
    );
    const verification = continueWith(
      result.continue_with,
      "show_verification_ui",
    );
    const cookies = signedIn(result);
    if (verification)
      cookies.push(
        cookie(
          names.flow,
          `verification:${verification.flow.id}`,
          FLOW_MAX_AGE,
        ),
      );
    return json(
      200,
      {
        user: result.session ? userOf(result.session, [], product) : null,
        ...(verification
          ? { verification: { flowId: verification.flow.id } }
          : {}),
      },
      cookies,
    );
  }

  async function sendOtp(request: Request): Promise<Response> {
    const b = await body(request);
    const email = str(b, "email");
    const type = str(b, "type") as OtpType;
    const kind = FLOW_OF_TYPE[type];
    if (!email || !kind)
      return failure("invalid_input", 400, "email and type are required");
    const flow = await kratos.start(kind);
    const submitted = await kratos.submit(kind, flow.id, {
      method: "code",
      ...(kind === "login" ? { identifier: email } : { email }),
    });
    if (!codeWasSent(submitted) && submitted.state !== "sent_email") {
      return failure("invalid_input", 400, "the code could not be sent");
    }
    return json(200, { sent: true }, [
      cookie(names.flow, `${kind}:${flow.id}`, FLOW_MAX_AGE),
    ]);
  }

  function pendingFlow(headers: Headers, kind: string): string | null {
    const raw = readCookie(headers, names.flow) ?? "";
    const [k, id] = raw.split(":");
    return k === kind && id ? id : null;
  }

  async function signInOtp(request: Request): Promise<Response> {
    const b = await body(request);
    const email = str(b, "email");
    const code = str(b, "otp");
    const flowId = pendingFlow(request.headers, "login");
    if (!flowId) return failure("flow_expired", 410);
    if (!email || !code)
      return failure("invalid_input", 400, "email and otp are required");
    const result = await kratos.submit<KratosSessionResult>("login", flowId, {
      method: "code",
      identifier: email,
      code,
    });
    return json(
      200,
      { user: result.session ? userOf(result.session, [], product) : null },
      signedIn(result),
    );
  }

  async function verifyEmail(request: Request): Promise<Response> {
    const b = await body(request);
    const code = str(b, "otp");
    const flowId = pendingFlow(request.headers, "verification");
    if (!flowId) return failure("flow_expired", 410);
    if (!code) return failure("invalid_input", 400, "otp is required");
    const flow = await kratos.submit("verification", flowId, {
      method: "code",
      code,
    });
    if (flow.state !== "passed_challenge") return failure("invalid_code", 400);
    return json(200, { verified: true }, [clearCookie(names.flow, secure)]);
  }

  async function resetPassword(request: Request): Promise<Response> {
    const b = await body(request);
    const code = str(b, "otp");
    const password = typeof b.password === "string" ? b.password : "";
    const flowId = pendingFlow(request.headers, "recovery");
    if (!flowId) return failure("flow_expired", 410);
    if (!code || !password)
      return failure("invalid_input", 400, "otp and password are required");
    const recovered = await kratos.submit<KratosSessionResult>(
      "recovery",
      flowId,
      { method: "code", code },
    );
    const token = continueWith(
      recovered.continue_with,
      "set_ory_session_token",
    )?.ory_session_token;
    const settings = continueWith(recovered.continue_with, "show_settings_ui");
    if (!token || !settings) return failure("invalid_code", 400);
    await kratos.submit(
      "settings",
      settings.flow.id,
      { method: "password", password },
      token,
    );
    return json(200, { reset: true }, [
      cookie(names.session, token, SESSION_MAX_AGE),
      clearCookie(names.flow, secure),
    ]);
  }

  async function signOut(request: Request): Promise<Response> {
    const token = readCookie(request.headers, names.session);
    if (token) await kratos.logout(token);
    return json(200, { signedOut: true }, [
      clearCookie(names.session, secure),
      clearCookie(names.token, secure),
      clearCookie(names.flow, secure),
    ]);
  }

  async function session(request: Request): Promise<Response> {
    const s = await getSession(request.headers);
    return json(200, s);
  }

  const routes: Record<string, (request: Request) => Promise<Response>> = {
    "POST sign-in/email": signInEmail,
    "POST sign-in/email-otp": signInOtp,
    "POST sign-up/email": signUpEmail,
    "POST email-otp/send-verification-otp": sendOtp,
    "POST email-otp/verify-email": verifyEmail,
    "POST email-otp/reset-password": resetPassword,
    "POST sign-out": signOut,
    "GET get-session": session,
  };

  async function handler(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname.replace(/\/$/, "");
    const suffix = path.slice(path.indexOf("/api/auth/") + "/api/auth/".length);
    if (suffix === "sign-in/social") return failure("not_supported", 501);
    const route = routes[`${request.method} ${suffix}`];
    if (!route) return failure("not_found", 404);
    try {
      return await route(request);
    } catch (error) {
      return failed(error);
    }
  }

  function coreProxy(options: UrbangateCoreProxyOptions) {
    const coreUrl = options.coreUrl.replace(/\/$/, "");
    return async (request: Request): Promise<Response> => {
      let s: UrbangateSession | null;
      let token: AccessToken | null;
      try {
        token = await accessToken(request.headers);
        s = token
          ? await getSession(
              new Headers({ ...cookieHeader(request.headers, names, token) }),
            )
          : null;
      } catch {
        return failure("identity_provider_unavailable", 503);
      }
      if (!token || !s) return failure("sign_in_required", 401);
      if (options.adminOnly && s.user.role !== "admin")
        return failure("forbidden", 403);
      const url = new URL(request.url);
      const headers = forwardHeaders(request.headers);
      headers.set("authorization", `Bearer ${token.token}`);
      const upstream = await fetch(`${coreUrl}${url.pathname}${url.search}`, {
        method: request.method,
        headers,
        body:
          request.method === "GET" || request.method === "HEAD"
            ? undefined
            : request.body,
        duplex: "half",
        redirect: "manual",
      } as RequestInit);
      const out = forwardHeaders(upstream.headers);
      if (token.setCookie) out.append("set-cookie", token.setCookie);
      return new Response(upstream.body, {
        status: upstream.status,
        headers: out,
      });
    };
  }

  return { handler, getSession, accessToken, coreProxy };
}

// A token just exchanged is not in the request's cookie yet; the session
// lookup reads the roles off it as if it were.
function cookieHeader(
  headers: Headers,
  names: { session: string; token: string },
  token: AccessToken,
) {
  const session = readCookie(headers, names.session) ?? "";
  return {
    cookie: `${names.session}=${encodeURIComponent(session)}; ${names.token}=${encodeURIComponent(token.token)}`,
  };
}
