import type { Fetch } from "./kratos";

export interface ExchangeConfig {
  product: string;
  issuerUrl: string;
  provisioner: { clientId: string; clientSecret: string };
  admin: { clientId: string; clientSecret: string };
}

export interface PersonToken {
  accessToken: string;
  expiresAt: number;
  identityId: string;
  roles: Array<string>;
}

export type ExchangeOutcome =
  | { status: "ok"; token: PersonToken }
  | { status: "session_gone" }
  | { status: "inactive" }
  | { status: "unavailable"; reason: string };

const JWT_BEARER = "urn:ietf:params:oauth:grant-type:jwt-bearer";
const EXCHANGE_SCOPE = "urbangate:sessions:exchange";
const REFRESH_MARGIN_MS = 60_000;

function basic(id: string, secret: string) {
  return "Basic " + btoa(`${id}:${secret}`);
}

/**
 * Turns a Kratos session token into the person's Hydra access token, in the
 * three calls ADR 0009 of urbangate describes: a machine token for the
 * provisioner, the assertion from urbangate, the token from Hydra with the
 * product's own admin client. Hydra issues no refresh token on this grant,
 * so the caller exchanges again when the token nears its end.
 */
export class Exchange {
  private machine: { token: string; expiresAt: number } | null = null;

  private readonly config: ExchangeConfig;
  private readonly fetchImpl: Fetch;

  constructor(config: ExchangeConfig, fetchImpl: Fetch = fetch) {
    this.config = config;
    this.fetchImpl = fetchImpl;
  }

  needsRefresh(token: PersonToken | null, now = Date.now()): boolean {
    return !token || token.expiresAt - now < REFRESH_MARGIN_MS;
  }

  async exchange(sessionToken: string): Promise<ExchangeOutcome> {
    const machine = await this.machineToken();
    if (!machine) return { status: "unavailable", reason: "machine_token" };
    const url = new URL(
      "/api/machine/sessions/exchange",
      this.config.issuerUrl,
    );
    let res: Response;
    try {
      res = await this.fetchImpl(url, {
        method: "POST",
        headers: {
          authorization: `Bearer ${machine}`,
          "content-type": "application/json",
        },
        body: JSON.stringify({ session_token: sessionToken }),
      });
    } catch {
      return { status: "unavailable", reason: "exchange" };
    }
    if (res.status === 401) {
      const body = (await res.json().catch(() => ({}))) as { error?: string };
      // Our own credential was refused: fetch a fresh one next time.
      if (body.error === "invalid_token") this.machine = null;
      return body.error === "session"
        ? { status: "session_gone" }
        : { status: "unavailable", reason: "exchange_401" };
    }
    if (res.status === 403) return { status: "inactive" };
    if (!res.ok)
      return { status: "unavailable", reason: `exchange_${res.status}` };
    const answer = (await res.json()) as {
      assertion: string;
      identity_id: string;
      roles: Array<string>;
    };
    return this.hydraToken(answer);
  }

  private async hydraToken(answer: {
    assertion: string;
    identity_id: string;
    roles: Array<string>;
  }): Promise<ExchangeOutcome> {
    const form = new URLSearchParams({
      grant_type: JWT_BEARER,
      assertion: answer.assertion,
      audience: this.config.product,
    });
    let res: Response;
    try {
      res = await this.fetchImpl(
        new URL("/oauth2/token", this.config.issuerUrl),
        {
          method: "POST",
          headers: {
            "content-type": "application/x-www-form-urlencoded",
            authorization: basic(
              this.config.admin.clientId,
              this.config.admin.clientSecret,
            ),
          },
          body: form,
        },
      );
    } catch {
      return { status: "unavailable", reason: "hydra" };
    }
    if (!res.ok)
      return { status: "unavailable", reason: `hydra_${res.status}` };
    const body = (await res.json()) as {
      access_token: string;
      expires_in: number;
    };
    return {
      status: "ok",
      token: {
        accessToken: body.access_token,
        expiresAt: Date.now() + body.expires_in * 1000,
        identityId: answer.identity_id,
        roles: answer.roles,
      },
    };
  }

  private async machineToken(): Promise<string | null> {
    if (
      this.machine &&
      this.machine.expiresAt - Date.now() > REFRESH_MARGIN_MS
    ) {
      return this.machine.token;
    }
    let res: Response;
    try {
      res = await this.fetchImpl(
        new URL("/oauth2/token", this.config.issuerUrl),
        {
          method: "POST",
          headers: {
            "content-type": "application/x-www-form-urlencoded",
            authorization: basic(
              this.config.provisioner.clientId,
              this.config.provisioner.clientSecret,
            ),
          },
          body: new URLSearchParams({
            grant_type: "client_credentials",
            scope: EXCHANGE_SCOPE,
            audience: "urbangate",
          }),
        },
      );
    } catch {
      return null;
    }
    if (!res.ok) return null;
    const body = (await res.json()) as {
      access_token: string;
      expires_in: number;
    };
    this.machine = {
      token: body.access_token,
      expiresAt: Date.now() + body.expires_in * 1000,
    };
    return this.machine.token;
  }
}

export function decodeToken(
  raw: string,
): { identityId: string; roles: Array<string>; expiresAt: number } | null {
  const parts = raw.split(".");
  if (parts.length !== 3) return null;
  try {
    const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const payload = JSON.parse(decodeURIComponent(escape(atob(b64)))) as {
      sub?: string;
      roles?: unknown;
      exp?: number;
    };
    if (!payload.sub || !payload.exp) return null;
    return {
      identityId: payload.sub,
      roles: Array.isArray(payload.roles)
        ? payload.roles.filter((r): r is string => typeof r === "string")
        : [],
      expiresAt: payload.exp * 1000,
    };
  } catch {
    return null;
  }
}
