import type { Fetch } from "./kratos";

export interface VerifiedToken {
  identityId: string;
  roles: Array<string>;
  expiresAt: number;
}

export class JwksUnavailable extends Error {}

interface Jwk extends JsonWebKey {
  kid?: string;
}

const CACHE_MS = 10 * 60_000;
const UNKNOWN_KID_COOLDOWN_MS = 30_000;

function bytesOf(b64url: string): Uint8Array<ArrayBuffer> {
  const b64 = b64url.replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

function jsonOf(b64url: string): Record<string, unknown> {
  return JSON.parse(new TextDecoder().decode(bytesOf(b64url)));
}

/**
 * Verifies Hydra's RS256 access tokens against the issuer's JWKS, as the
 * cores do, so a token cookie the browser forged never reads as a session.
 */
export class Jwks {
  private keys = new Map<string, CryptoKey>();
  private fetchedAt = 0;
  private attemptedAt = -Infinity;
  private refreshing: Promise<void> | null = null;

  private readonly issuerUrl: string;
  private readonly audience: string;
  private readonly fetchImpl: Fetch;

  constructor(issuerUrl: string, audience: string, fetchImpl: Fetch = fetch) {
    this.issuerUrl = issuerUrl.replace(/\/$/, "");
    this.audience = audience;
    this.fetchImpl = fetchImpl;
  }

  async verify(raw: string, now = Date.now()): Promise<VerifiedToken | null> {
    const parts = raw.split(".");
    if (parts.length !== 3) return null;
    let header: Record<string, unknown>;
    let payload: Record<string, unknown>;
    try {
      header = jsonOf(parts[0]);
      payload = jsonOf(parts[1]);
    } catch {
      return null;
    }
    if (header.alg !== "RS256" || typeof header.kid !== "string") return null;
    const key = await this.key(header.kid, now);
    if (!key) return null;
    const signed = new TextEncoder().encode(`${parts[0]}.${parts[1]}`);
    const valid = await crypto.subtle.verify(
      "RSASSA-PKCS1-v1_5",
      key,
      bytesOf(parts[2]),
      signed,
    );
    if (!valid) return null;
    const aud = Array.isArray(payload.aud) ? payload.aud : [payload.aud];
    const exp = typeof payload.exp === "number" ? payload.exp * 1000 : 0;
    if (
      String(payload.iss ?? "").replace(/\/$/, "") !== this.issuerUrl ||
      !aud.includes(this.audience) ||
      exp <= now ||
      typeof payload.sub !== "string"
    )
      return null;
    const ext = payload.ext as { roles?: unknown } | undefined;
    const roles = Array.isArray(payload.roles) ? payload.roles : ext?.roles;
    return {
      identityId: payload.sub,
      roles: Array.isArray(roles)
        ? roles.filter((r): r is string => typeof r === "string")
        : [],
      expiresAt: exp,
    };
  }

  // Every attempt, failed or not, opens the cooldown: while the issuer is
  // down, requests keep the keys they have instead of all asking it again.
  private async key(kid: string, now: number): Promise<CryptoKey | null> {
    const wanted = now - this.fetchedAt > CACHE_MS || !this.keys.has(kid);
    const cooling = now - this.attemptedAt < UNKNOWN_KID_COOLDOWN_MS;
    if (wanted && !cooling) {
      this.attemptedAt = now;
      try {
        await this.refresh(now);
      } catch (error) {
        if (this.keys.size === 0) throw error;
      }
    } else if (wanted && this.keys.size === 0 && this.fetchedAt === 0) {
      throw new JwksUnavailable("jwks not loaded yet");
    }
    return this.keys.get(kid) ?? null;
  }

  private refresh(now: number): Promise<void> {
    this.refreshing ??= this.load(now).finally(() => {
      this.refreshing = null;
    });
    return this.refreshing;
  }

  private async load(now: number): Promise<void> {
    let res: Response;
    try {
      res = await this.fetchImpl(`${this.issuerUrl}/.well-known/jwks.json`);
    } catch {
      throw new JwksUnavailable("jwks unreachable");
    }
    if (!res.ok) throw new JwksUnavailable(`jwks ${res.status}`);
    const body = (await res.json()) as { keys?: Array<Jwk> };
    const keys = new Map<string, CryptoKey>();
    for (const jwk of body.keys ?? []) {
      if (!jwk.kid || jwk.kty !== "RSA" || (jwk.use && jwk.use !== "sig"))
        continue;
      keys.set(
        jwk.kid,
        await crypto.subtle.importKey(
          "jwk",
          { kty: jwk.kty, n: jwk.n, e: jwk.e },
          { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
          false,
          ["verify"],
        ),
      );
    }
    this.keys = keys;
    this.fetchedAt = now;
  }
}
