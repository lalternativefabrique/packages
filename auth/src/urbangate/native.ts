import { useCallback, useEffect, useState } from "react";
import type { AuthClientResult } from "../types";
import type {
  UrbangateAuthClient,
  UrbangateClientSession,
} from "./client.ts";

export interface NativeStorage {
  getItem(key: string): Promise<string | null>;
  setItem(key: string, value: string): Promise<void>;
  deleteItem(key: string): Promise<void>;
}

export interface UrbangateNativeClientConfig {
  baseURL: string;
  product: string;
  storage: NativeStorage;
  fetch?: typeof fetch;
}

export interface NativeSessionState {
  data: UrbangateClientSession | null;
  isPending: boolean;
  error: AuthClientResult["error"];
  refetch(): Promise<void>;
}

export type UrbangateNativeClient = Omit<UrbangateAuthClient, "useSession"> & {
  cookieHeader(): Promise<string>;
  fetch(path: string, init?: RequestInit): Promise<Response>;
  useSession(): NativeSessionState;
};

export interface ParsedCookie {
  name: string;
  value: string;
  deleted: boolean;
}

// React Native joins repeated Set-Cookie headers with ", ", and an Expires
// date carries a comma too, so the join is split only where a pair begins.
export function splitSetCookie(joined: string): Array<string> {
  return joined
    .split(/,(?=\s*[A-Za-z0-9_.-]+=)/)
    .map((c) => c.trim())
    .filter(Boolean);
}

export function setCookiesOf(headers: Headers): Array<string> {
  const withList = headers as Headers & { getSetCookie?: () => Array<string> };
  const list = withList.getSetCookie?.();
  if (list && list.length > 0) return list;
  const joined = headers.get("set-cookie");
  return joined ? splitSetCookie(joined) : [];
}

export function parseSetCookie(raw: string, now = Date.now()): ParsedCookie {
  const [pair = "", ...attrs] = raw.split(";");
  const eq = pair.indexOf("=");
  const name = (eq === -1 ? pair : pair.slice(0, eq)).trim();
  const encoded = eq === -1 ? "" : pair.slice(eq + 1).trim();
  let value = encoded;
  try {
    value = decodeURIComponent(encoded);
  } catch {
    value = encoded;
  }
  let deleted = value === "";
  for (const attr of attrs) {
    const [k = "", v = ""] = attr.split("=").map((s) => s.trim());
    const key = k.toLowerCase();
    if (key === "max-age" && Number(v) <= 0) deleted = true;
    if (key === "expires") {
      const at = Date.parse(v);
      if (!Number.isNaN(at) && at <= now) deleted = true;
    }
  }
  return { name, value, deleted };
}

type Result<T = unknown> = { data: T | null; error: AuthClientResult["error"] };

/**
 * The product web's /api/auth routes for an Expo app. React Native's fetch
 * keeps no reliable cookie jar, so the cookies the web sets are held in the
 * injected storage and replayed by hand with `credentials: "omit"`.
 */
export function createUrbangateNativeClient(
  config: UrbangateNativeClientConfig,
): UrbangateNativeClient {
  const base = config.baseURL.replace(/\/$/, "");
  const doFetch = config.fetch ?? fetch;
  const names = {
    session: `${config.product}_session`,
    token: `${config.product}_token`,
    flow: `${config.product}_flow`,
  };
  const held = new Set<string>(Object.values(names));
  const listeners = new Set<() => void>();

  async function cookieHeader(): Promise<string> {
    const pairs: Array<string> = [];
    for (const name of held) {
      const value = await config.storage.getItem(name);
      if (value) pairs.push(`${name}=${encodeURIComponent(value)}`);
    }
    return pairs.join("; ");
  }

  async function keep(headers: Headers): Promise<boolean> {
    let sessionChanged = false;
    for (const raw of setCookiesOf(headers)) {
      const cookie = parseSetCookie(raw);
      if (!held.has(cookie.name)) continue;
      if (cookie.name === names.session) sessionChanged = true;
      if (cookie.deleted) await config.storage.deleteItem(cookie.name);
      else await config.storage.setItem(cookie.name, cookie.value);
    }
    return sessionChanged;
  }

  function notify() {
    for (const listener of listeners) listener();
  }

  async function authorizedFetch(
    path: string,
    init: RequestInit = {},
  ): Promise<Response> {
    const headers = new Headers(init.headers);
    const cookie = await cookieHeader();
    if (cookie) headers.set("cookie", cookie);
    const res = await doFetch(`${base}${path}`, {
      ...init,
      headers,
      credentials: "omit",
    });
    if (await keep(res.headers)) notify();
    return res;
  }

  async function call<T = unknown>(
    method: "GET" | "POST",
    path: string,
    body?: unknown,
  ): Promise<Result<T>> {
    try {
      const res = await authorizedFetch(`/api/auth/${path}`, {
        method,
        headers: {
          accept: "application/json",
          ...(body === undefined ? {} : { "content-type": "application/json" }),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const parsed = (await res.json().catch(() => null)) as
        | { error?: AuthClientResult["error"] }
        | T
        | null;
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

  const getSession = () =>
    call<UrbangateClientSession | null>("GET", "get-session");

  async function signOut(): Promise<AuthClientResult> {
    const result = await call("POST", "sign-out");
    for (const name of held) await config.storage.deleteItem(name);
    notify();
    return result;
  }

  return {
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
    signOut,
    updateUser: (input) => call("POST", "update-user", input),
    changePassword: (input) => call("POST", "change-password", input),
    getSession,
    cookieHeader,
    fetch: authorizedFetch,
    useSession() {
      const [state, setState] = useState<Omit<NativeSessionState, "refetch">>(
        { data: null, isPending: true, error: null },
      );
      const refetch = useCallback(async () => {
        const { data, error } = await getSession();
        setState({ data, isPending: false, error: error ?? null });
      }, []);
      useEffect(() => {
        let live = true;
        const load = () => {
          void getSession().then(({ data, error }) => {
            if (live) setState({ data, isPending: false, error: error ?? null });
          });
        };
        load();
        listeners.add(load);
        return () => {
          live = false;
          listeners.delete(load);
        };
      }, []);
      return { ...state, refetch };
    },
  };
}
