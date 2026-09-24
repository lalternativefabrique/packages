import { useEffect, useState } from "react";
import type { AuthClientResult, AuthClientSurface } from "../types";

export interface UrbangateAuthClientConfig {
  baseURL?: string;
  /** Called when a core call is refused and the session cannot renew its token. */
  onSignedOut?: () => void;
}

export interface UrbangateClientUser {
  id: string;
  identityId: string;
  email: string;
  emailVerified: boolean;
  name: string;
  role: "admin" | "user";
}

export interface UrbangateClientSession {
  user: UrbangateClientUser;
  session: { expiresAt: string };
}

export interface SessionState {
  data: UrbangateClientSession | null;
  isPending: boolean;
  error: { message?: string; status?: number } | null;
}

export type UrbangateAuthClient = Omit<
  AuthClientSurface,
  "admin" | "signIn"
> & {
  signIn: AuthClientSurface["signIn"] & {
    emailOtp(input: { email: string; otp: string }): Promise<AuthClientResult>;
    urbangate(input?: { callbackURL?: string }): Promise<void>;
  };
  secondFactor: {
    verify(input: {
      code: string;
      password: string;
    }): Promise<AuthClientResult>;
  };
  signOut(): Promise<AuthClientResult>;
  updateUser(input: { name: string }): Promise<AuthClientResult>;
  changePassword(input: {
    currentPassword: string;
    newPassword: string;
    revokeOtherSessions?: boolean;
  }): Promise<AuthClientResult>;
  getSession(): Promise<{
    data: UrbangateClientSession | null;
    error: AuthClientResult["error"];
  }>;
  useSession(): SessionState;
  /**
   * `fetch` for the product's core: on a 401 it renews the person's token
   * once and replays the request. A renewal refused signs out through
   * `onSignedOut`; one urbangate cannot answer comes back as its 503.
   */
  fetch(input: string | URL, init?: RequestInit): Promise<Response>;
};

type Result<T = unknown> = { data: T | null; error: AuthClientResult["error"] };

export function createUrbangateAuthClient(
  config: UrbangateAuthClientConfig = {},
): UrbangateAuthClient {
  const base = (
    config.baseURL ??
    (typeof window !== "undefined" ? window.location.origin : "")
  ).replace(/\/$/, "");

  async function call<T = unknown>(
    method: "GET" | "POST",
    path: string,
    body?: unknown,
  ): Promise<Result<T>> {
    try {
      const res = await fetch(`${base}/api/auth/${path}`, {
        method,
        credentials: "include",
        headers:
          body === undefined ? {} : { "content-type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const parsed = (await res.json().catch(() => null)) as
        { error?: AuthClientResult["error"] } | T | null;
      if (!res.ok) {
        const error = (parsed as { error?: AuthClientResult["error"] } | null)
          ?.error;
        return {
          data: null,
          error: error ?? { status: res.status, message: res.statusText },
        };
      }
      return { data: parsed as T, error: null };
    } catch (err) {
      return {
        data: null,
        error: {
          message: err instanceof Error ? err.message : "network",
          status: 0,
        },
      };
    }
  }

  let renewing: Promise<Response> | null = null;
  const renew = () => {
    renewing ??= fetch(`${base}/api/auth/core-token`, {
      credentials: "include",
    }).finally(() => {
      renewing = null;
    });
    return renewing;
  };

  async function coreFetch(
    input: string | URL,
    init: RequestInit = {},
  ): Promise<Response> {
    const send = () => fetch(input, { ...init, credentials: "include" });
    const first = await send();
    if (first.status !== 401) return first;
    let renewed: Response;
    try {
      renewed = await renew();
    } catch {
      return first;
    }
    if (renewed.ok) return send();
    if (renewed.status === 401) config.onSignedOut?.();
    return renewed.status === 401 ? first : renewed.clone();
  }

  const client: UrbangateAuthClient = {
    signIn: {
      email: (input) => call("POST", "sign-in/email", input),
      emailOtp: (input) => call("POST", "sign-in/email-otp", input),
      social: async () => ({ error: { code: "not_supported", status: 501 } }),
      urbangate: async (input = {}) => {
        const q = input.callbackURL
          ? `?callbackURL=${encodeURIComponent(input.callbackURL)}`
          : "";
        window.location.assign(`${base}/api/auth/sign-in/urbangate${q}`);
      },
    },
    signUp: {
      email: (input) => call("POST", "sign-up/email", input),
    },
    emailOtp: {
      sendVerificationOtp: (input) =>
        call("POST", "email-otp/send-verification-otp", input),
      verifyEmail: (input) => call("POST", "email-otp/verify-email", input),
      resetPassword: (input) => call("POST", "email-otp/reset-password", input),
    },
    secondFactor: {
      verify: (input) => call("POST", "second-factor/verify", input),
    },
    signOut: () => call("POST", "sign-out"),
    updateUser: (input) => call("POST", "update-user", input),
    changePassword: (input) => call("POST", "change-password", input),
    getSession: () => call<UrbangateClientSession | null>("GET", "get-session"),
    fetch: coreFetch,
    useSession() {
      const [state, setState] = useState<SessionState>({
        data: null,
        isPending: true,
        error: null,
      });
      useEffect(() => {
        let live = true;
        void client.getSession().then(({ data, error }) => {
          if (live) setState({ data, isPending: false, error: error ?? null });
        });
        return () => {
          live = false;
        };
      }, []);
      return state;
    },
  };
  return client;
}
