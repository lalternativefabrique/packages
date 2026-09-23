import { useEffect, useState } from "react";
import type { AuthClientResult, AuthClientSurface } from "../types";

export interface UrbangateAuthClientConfig {
  baseURL?: string;
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
  };
  secondFactor: {
    verify(input: {
      code: string;
      password: string;
    }): Promise<AuthClientResult>;
  };
  signOut(): Promise<AuthClientResult>;
  getSession(): Promise<{
    data: UrbangateClientSession | null;
    error: AuthClientResult["error"];
  }>;
  useSession(): SessionState;
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

  const client: UrbangateAuthClient = {
    signIn: {
      email: (input) => call("POST", "sign-in/email", input),
      emailOtp: (input) => call("POST", "sign-in/email-otp", input),
      social: async () => ({ error: { code: "not_supported", status: 501 } }),
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
    getSession: () => call<UrbangateClientSession | null>("GET", "get-session"),
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
