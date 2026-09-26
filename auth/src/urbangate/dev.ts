import { runAccountDeletion } from "../account-deletion.ts";
import { forwardHeaders } from "../core-proxy.ts";
import { clearCookie, readCookie, serializeCookie } from "./cookies.ts";
import { coreTarget, keptCookies, onCore, relayedHeaders } from "./relay.ts";
import type {
  AccessToken,
  CoreCall,
  Guarded,
  UrbangateAuth,
  UrbangateAuthConfig,
  UrbangateCoreProxyOptions,
  UrbangateSession,
  UrbangateUser,
} from "./server.ts";

export interface DevAuthConfig {
  product: string;
  /**
   * The web as the core reaches it, e.g. http://web:5273: the `iss` of the
   * tokens it signs. The core trusts it as websession's `Web` issuer and
   * reads the keys at `<issuer>/api/auth/jwks`.
   */
  issuer: string;
  coreUrl?: string;
  /** Addresses that sign in as this product's admin. */
  admins?: Array<string>;
  cookie?: { name?: string; secure?: boolean };
  onAccountOpened?: UrbangateAuthConfig["onAccountOpened"];
  accountDeletion?: UrbangateAuthConfig["accountDeletion"];
  coreTokenInBody?: boolean;
}

const SESSION_MAX_AGE = 30 * 24 * 60 * 60;
const TOKEN_MAX_AGE = 15 * 60;
const KID = "dev";

// PKCS#8 wrapping of a raw Ed25519 seed. The seed is fixed so a session
// survives a restart of the dev server; the key signs nothing outside one.
const ED25519_PKCS8_PREFIX = [
  0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04,
  0x22, 0x04, 0x20,
];

const text = new TextEncoder();

function b64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function bytesOf(value: string): Uint8Array<ArrayBuffer> {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

async function keysFor(product: string) {
  const seed = new Uint8Array(
    await crypto.subtle.digest("SHA-256", text.encode(`${product} dev auth`)),
  );
  const pkcs8 = new Uint8Array([...ED25519_PKCS8_PREFIX, ...seed]);
  const privateKey = await crypto.subtle.importKey(
    "pkcs8",
    pkcs8,
    { name: "Ed25519" },
    true,
    ["sign"],
  );
  const { d: _d, ...publicJwk } = await crypto.subtle.exportKey(
    "jwk",
    privateKey,
  );
  const publicKey = await crypto.subtle.importKey(
    "jwk",
    { ...publicJwk, key_ops: ["verify"] },
    { name: "Ed25519" },
    true,
    ["verify"],
  );
  return {
    privateKey,
    publicKey,
    jwk: { ...publicJwk, key_ops: undefined, kid: KID, alg: "EdDSA", use: "sig" },
  };
}

async function identityOf(email: string): Promise<string> {
  const h = Array.from(
    new Uint8Array(await crypto.subtle.digest("SHA-256", text.encode(email))),
    (b) => b.toString(16).padStart(2, "0"),
  ).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20, 32)}`;
}

function json(status: number, body: unknown, cookies: Array<string> = []) {
  const headers = new Headers({ "content-type": "application/json" });
  for (const c of cookies) headers.append("set-cookie", c);
  return new Response(JSON.stringify(body), { status, headers });
}

function failure(code: string, status: number, message?: string): Response {
  return json(status, { error: { code, status, ...(message ? { message } : {}) } });
}

/**
 * Stands in for createUrbangateAuth on a dev stack that runs no urbangate:
 * any address and any password (or any code) open a session, and the tokens
 * the core receives are signed here. The routes, the cookies and the
 * UrbangateAuth surface are the ones the product already uses, so clients
 * and routes do not change. Never in production: it trusts whoever asks.
 */
export function createDevAuth(config: DevAuthConfig): UrbangateAuth {
  const { product } = config;
  const issuer = config.issuer.replace(/\/$/, "");
  const sessionCookie = config.cookie?.name ?? `${product}_session`;
  const secure = config.cookie?.secure ?? issuer.startsWith("https://");
  const admins = new Set(config.admins?.map((a) => a.trim().toLowerCase()));
  const keys = keysFor(product);

  async function sign(user: UrbangateUser, maxAge: number): Promise<string> {
    const now = Math.floor(Date.now() / 1000);
    const head = b64url(text.encode(JSON.stringify({ alg: "EdDSA", typ: "JWT", kid: KID })));
    const body = b64url(
      text.encode(
        JSON.stringify({
          iss: issuer,
          aud: product,
          sub: user.identityId,
          identityId: user.identityId,
          email: user.email,
          name: user.name,
          role: user.role,
          iat: now,
          exp: now + maxAge,
        }),
      ),
    );
    const signature = await crypto.subtle.sign(
      { name: "Ed25519" },
      (await keys).privateKey,
      text.encode(`${head}.${body}`),
    );
    return `${head}.${body}.${b64url(new Uint8Array(signature))}`;
  }

  async function read(raw: string): Promise<(UrbangateUser & { exp: number }) | null> {
    const [head, body, signature] = raw.split(".");
    if (!head || !body || !signature) return null;
    const valid = await crypto.subtle
      .verify(
        { name: "Ed25519" },
        (await keys).publicKey,
        bytesOf(signature),
        text.encode(`${head}.${body}`),
      )
      .catch(() => false);
    if (!valid) return null;
    const claims = JSON.parse(new TextDecoder().decode(bytesOf(body)));
    if (claims.iss !== issuer || claims.exp * 1000 < Date.now()) return null;
    return {
      id: claims.identityId,
      identityId: claims.identityId,
      email: claims.email,
      emailVerified: true,
      name: claims.name,
      role: claims.role === "admin" ? "admin" : "user",
      exp: claims.exp,
    };
  }

  async function userFor(email: string, name?: string): Promise<UrbangateUser> {
    const address = email.trim().toLowerCase();
    const identityId = await identityOf(address);
    return {
      id: identityId,
      identityId,
      email: address,
      emailVerified: true,
      name: name ?? address.split("@")[0],
      role: admins.has(address) ? "admin" : "user",
    };
  }

  const signedIn = async (user: UrbangateUser) => [
    serializeCookie(sessionCookie, await sign(user, SESSION_MAX_AGE), {
      maxAge: SESSION_MAX_AGE,
      secure,
    }),
  ];

  async function getSession(headers: Headers): Promise<UrbangateSession | null> {
    const raw = readCookie(headers, sessionCookie);
    const read_ = raw ? await read(raw) : null;
    if (!read_) return null;
    const { exp, ...user } = read_;
    return { user, session: { expiresAt: new Date(exp * 1000).toISOString() } };
  }

  async function accessToken(headers: Headers): Promise<AccessToken | null> {
    const s = await getSession(headers);
    return s ? { token: await sign(s.user, TOKEN_MAX_AGE) } : null;
  }

  async function body(request: Request): Promise<Record<string, unknown>> {
    const raw = await request.json().catch(() => null);
    return raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  }
  const str = (b: Record<string, unknown>, key: string) =>
    typeof b[key] === "string" ? b[key].trim() : "";

  async function open(request: Request, user: UrbangateUser, opened: boolean) {
    const cookies = await signedIn(user);
    if (opened && config.onAccountOpened) {
      try {
        const extra = await config.onAccountOpened({
          user,
          headers: new Headers({
            cookie: cookies[0].split(";")[0],
          }),
          request,
        });
        cookies.push(...(extra ?? []));
      } catch (error) {
        console.warn(
          "[auth] onAccountOpened failed:",
          error instanceof Error ? error.message : error,
        );
      }
    }
    return json(200, { user }, cookies);
  }

  const withPassword = (opened: boolean) => async (request: Request) => {
    const b = await body(request);
    const email = str(b, "email");
    if (!email || !str(b, "password"))
      return failure("invalid_input", 400, "email and password are required");
    return open(request, await userFor(email, str(b, "name") || undefined), opened);
  };

  async function signInOtp(request: Request) {
    const b = await body(request);
    const email = str(b, "email");
    if (!email || !str(b, "otp"))
      return failure("invalid_input", 400, "email and otp are required");
    return open(request, await userFor(email), false);
  }

  async function updateUser(request: Request) {
    const s = await getSession(request.headers);
    if (!s) return failure("sign_in_required", 401);
    const name = str(await body(request), "name");
    const user = { ...s.user, name: name || s.user.name };
    return json(200, { status: true }, await signedIn(user));
  }

  const signedOut = () => [clearCookie(sessionCookie, secure)];

  async function deleteAccount(request: Request) {
    if (!config.accountDeletion) return failure("not_supported", 501);
    const s = await getSession(request.headers);
    if (!s) return failure("sign_in_required", 401);
    const report = await runAccountDeletion(
      config.accountDeletion({
        user: s.user,
        accessToken: await sign(s.user, TOKEN_MAX_AGE),
      }),
    );
    return report.deleted ? json(200, report, signedOut()) : json(502, report);
  }

  const routes: Partial<Record<string, (request: Request) => Promise<Response>>> = {
    "POST sign-in/email": withPassword(false),
    "POST sign-up/email": withPassword(true),
    "POST sign-in/email-otp": signInOtp,
    "POST email-otp/send-verification-otp": async () => json(200, { sent: true }),
    "POST email-otp/verify-email": async () => json(200, { verified: true }),
    "POST email-otp/reset-password": async () => json(200, { reset: true }),
    "POST second-factor/verify": async () => json(200, { reset: true }),
    "POST change-password": async () => json(200, { status: true, othersRevoked: false }),
    "POST update-user": updateUser,
    "POST sign-out": async () => json(200, { signedOut: true }, signedOut()),
    "POST delete-account": deleteAccount,
    "GET get-session": async (request) => json(200, await getSession(request.headers)),
    "GET core-token": async (request) => {
      const s = await getSession(request.headers);
      if (!s) return failure("sign_in_required", 401);
      const token = await sign(s.user, TOKEN_MAX_AGE);
      return json(
        200,
        config.coreTokenInBody
          ? { token, expires_at: new Date(Date.now() + TOKEN_MAX_AGE * 1000).toISOString() }
          : { refreshed: true },
      );
    },
    "GET profile": async (request) => {
      const s = await getSession(request.headers);
      if (!s) return failure("sign_in_required", 401);
      const u = s.user;
      return json(200, {
        user_id: u.identityId,
        email: u.email,
        name: u.name,
        avatar_url: "",
        roles: [u.role],
      });
    },
    "GET jwks": async () => json(200, { keys: [(await keys).jwk] }),
  };

  async function handler(request: Request): Promise<Response> {
    const path = new URL(request.url).pathname.replace(/\/$/, "");
    const suffix = path.slice(path.indexOf("/api/auth/") + "/api/auth/".length);
    const route = routes[`${request.method} ${suffix}`];
    return route ? route(request) : failure("not_found", 404);
  }

  async function guard(headers: Headers, admin: boolean): Promise<Guarded> {
    const s = await getSession(headers);
    if (!s) return { response: failure("sign_in_required", 401) };
    if (admin && s.user.role !== "admin")
      return { response: failure("forbidden", 403) };
    return { session: s, setCookies: [] };
  }

  async function coreFetch(
    headers: Headers,
    path: string,
    init: RequestInit = {},
  ): Promise<CoreCall> {
    if (!config.coreUrl) throw new Error("coreFetch needs the auth's coreUrl");
    const target = onCore(config.coreUrl.replace(/\/$/, ""), path);
    if (!target) throw new Error(`coreFetch path leaves the core: ${path}`);
    const token = await accessToken(headers);
    if (!token) return { status: "signed_out" };
    const h = new Headers(init.headers);
    h.set("authorization", `Bearer ${token.token}`);
    try {
      const response = await fetch(target, {
        ...init,
        headers: h,
        signal: init.signal ?? AbortSignal.timeout(10_000),
      });
      return { status: "ok", response, setCookies: [] };
    } catch {
      return { status: "unavailable", cause: "core" };
    }
  }

  function coreProxy(options: UrbangateCoreProxyOptions = {}) {
    const base = options.coreUrl ?? config.coreUrl;
    if (!base) throw new Error("coreProxy needs a coreUrl");
    const coreUrl = base.replace(/\/$/, "");
    const forwardAnonymous = options.anonymous === "forward";
    return async (request: Request): Promise<Response> => {
      const target = coreTarget(coreUrl, new URL(request.url), options.stripPrefix);
      if (!target) return failure("not_found", 404);
      const headers = forwardHeaders(request.headers);
      const cookies = keptCookies(request.headers, options.forwardCookies);
      if (cookies) headers.set("cookie", cookies);
      const ownCredential = request.headers.get("authorization");
      if (forwardAnonymous && ownCredential) {
        headers.set("authorization", ownCredential);
      } else {
        const s = await getSession(request.headers);
        if (!s) {
          if (!forwardAnonymous) return failure("sign_in_required", 401);
        } else if (options.adminOnly && s.user.role !== "admin") {
          return failure("forbidden", 403);
        } else {
          headers.set("authorization", `Bearer ${await sign(s.user, TOKEN_MAX_AGE)}`);
        }
      }
      let upstream: Response;
      try {
        upstream = await fetch(target, {
          method: request.method,
          headers,
          body:
            request.method === "GET" || request.method === "HEAD"
              ? undefined
              : request.body,
          duplex: "half",
          redirect: "manual",
        } as RequestInit);
      } catch {
        return failure("core_unavailable", 502);
      }
      return new Response(upstream.body, {
        status: upstream.status,
        headers: relayedHeaders(upstream.headers),
      });
    };
  }

  return {
    handler,
    getSession,
    accessToken,
    coreProxy,
    coreFetch,
    requireSession: (headers) => guard(headers, false),
    requireAdmin: (headers) => guard(headers, true),
  };
}
