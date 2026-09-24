import type { Fetch } from "./kratos";

export interface ConsoleSsoConfig {
  product: string;
  issuerUrl: string;
  clientId: string;
  clientSecret: string;
  redirectUri: string;
}

export interface SsoTokens {
  accessToken: string;
  refreshToken: string;
}

export type SsoOutcome =
  | { status: "ok"; tokens: SsoTokens }
  | { status: "refused" }
  | { status: "unavailable" };

export interface SsoProfile {
  email: string;
  emailVerified: boolean;
  name: string;
}

export interface PendingSignIn {
  state: string;
  verifier: string;
  landing: string;
}

const SCOPE = "openid offline_access email profile";

function base64url(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function random(bytes = 32): string {
  return base64url(crypto.getRandomValues(new Uint8Array(bytes)));
}

async function challengeOf(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return base64url(new Uint8Array(digest));
}

export function localPath(raw: string | null, fallback: string): string {
  if (
    !raw ||
    !raw.startsWith("/") ||
    raw.startsWith("//") ||
    raw.includes("\\")
  )
    return fallback;
  return raw;
}

export function encodePending(p: PendingSignIn): string {
  return base64url(new TextEncoder().encode(JSON.stringify(p)));
}

export function decodePending(raw: string | undefined): PendingSignIn | null {
  if (!raw) return null;
  try {
    const b64 = raw.replace(/-/g, "+").replace(/_/g, "/");
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const p = JSON.parse(new TextDecoder().decode(bytes)) as PendingSignIn;
    return typeof p.state === "string" &&
      typeof p.verifier === "string" &&
      typeof p.landing === "string"
      ? p
      : null;
  } catch {
    return null;
  }
}

/**
 * The suite team's way into a console (urbangate ADR 0003): Hydra's
 * authorization code with PKCE, on the product's `-admin` client, which
 * authenticates with client_secret_post.
 */
export class ConsoleSso {
  private readonly inFlight = new Map<string, Promise<SsoOutcome>>();

  private readonly config: ConsoleSsoConfig;
  private readonly fetchImpl: Fetch;

  constructor(config: ConsoleSsoConfig, fetchImpl: Fetch = fetch) {
    this.config = config;
    this.fetchImpl = fetchImpl;
  }

  async start(
    landing: string,
  ): Promise<{ location: string; pending: PendingSignIn }> {
    const pending = { state: random(16), verifier: random(), landing };
    const url = new URL("/oauth2/auth", this.config.issuerUrl);
    url.search = new URLSearchParams({
      client_id: this.config.clientId,
      response_type: "code",
      redirect_uri: this.config.redirectUri,
      scope: SCOPE,
      audience: this.config.product,
      state: pending.state,
      code_challenge: await challengeOf(pending.verifier),
      code_challenge_method: "S256",
    }).toString();
    return { location: url.toString(), pending };
  }

  exchangeCode(code: string, verifier: string): Promise<SsoOutcome> {
    return this.token({
      grant_type: "authorization_code",
      code,
      code_verifier: verifier,
      redirect_uri: this.config.redirectUri,
    });
  }

  // Hydra rotates the refresh token on every use; two requests of the same
  // browser refreshing at once share one call rather than spend it twice.
  refresh(refreshToken: string): Promise<SsoOutcome> {
    const running = this.inFlight.get(refreshToken);
    if (running) return running;
    const call = this.token({
      grant_type: "refresh_token",
      refresh_token: refreshToken,
    }).finally(() => this.inFlight.delete(refreshToken));
    this.inFlight.set(refreshToken, call);
    return call;
  }

  async revoke(refreshToken: string): Promise<void> {
    await this.fetchImpl(new URL("/oauth2/revoke", this.config.issuerUrl), {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        token: refreshToken,
        token_type_hint: "refresh_token",
        client_id: this.config.clientId,
        client_secret: this.config.clientSecret,
      }),
    }).catch(() => undefined);
  }

  /** Null when Hydra refuses the token; throws when it cannot answer. */
  async profile(accessToken: string): Promise<SsoProfile | null> {
    const res = await this.fetchImpl(
      new URL("/userinfo", this.config.issuerUrl),
      { headers: { authorization: `Bearer ${accessToken}` } },
    );
    if (res.status === 401 || res.status === 403) return null;
    if (!res.ok) throw new Error(`userinfo ${res.status}`);
    const body = (await res.json()) as {
      email?: string;
      email_verified?: boolean;
      name?: string;
    };
    return {
      email: body.email ?? "",
      emailVerified: body.email_verified ?? false,
      name: body.name ?? "",
    };
  }

  private async token(grant: Record<string, string>): Promise<SsoOutcome> {
    let res: Response;
    try {
      res = await this.fetchImpl(
        new URL("/oauth2/token", this.config.issuerUrl),
        {
          method: "POST",
          headers: { "content-type": "application/x-www-form-urlencoded" },
          body: new URLSearchParams({
            ...grant,
            client_id: this.config.clientId,
            client_secret: this.config.clientSecret,
          }),
        },
      );
    } catch {
      return { status: "unavailable" };
    }
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as { error?: string };
      return body.error === "invalid_grant"
        ? { status: "refused" }
        : { status: "unavailable" };
    }
    const body = (await res.json()) as {
      access_token?: string;
      refresh_token?: string;
    };
    if (!body.access_token || !body.refresh_token)
      return { status: "unavailable" };
    return {
      status: "ok",
      tokens: {
        accessToken: body.access_token,
        refreshToken: body.refresh_token,
      },
    };
  }
}
