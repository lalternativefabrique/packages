import { useCallback, useEffect, useState } from "react";
import { KeysRequestError } from "./transport";
import type { AppKey, CreatedAppKey, KeysError, KeysTransport } from "./types";

export type KeysStatus = "loading" | "ready" | "working";

export interface UseAppKeys {
  keys: Array<AppKey>;
  status: KeysStatus;
  error: KeysError | null;
  created: CreatedAppKey | null;
  create: (label: string) => Promise<void>;
  revoke: (id: string) => Promise<void>;
  dismissCreated: () => void;
}

function errorOf(cause: unknown): KeysError {
  return cause instanceof KeysRequestError
    ? cause.error
    : { kind: "unavailable" };
}

export function useAppKeys(transport: KeysTransport): UseAppKeys {
  const [keys, setKeys] = useState<Array<AppKey>>([]);
  const [status, setStatus] = useState<KeysStatus>("loading");
  const [error, setError] = useState<KeysError | null>(null);
  const [created, setCreated] = useState<CreatedAppKey | null>(null);

  const load = useCallback(async () => {
    try {
      setKeys(await transport.list());
      setError(null);
    } catch (cause) {
      setError(errorOf(cause));
    } finally {
      setStatus("ready");
    }
  }, [transport]);

  useEffect(() => {
    let cancelled = false;
    transport
      .list()
      .then((rows) => {
        if (cancelled) return;
        setKeys(rows);
        setError(null);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(errorOf(cause));
      })
      .finally(() => {
        if (!cancelled) setStatus("ready");
      });
    return () => {
      cancelled = true;
    };
  }, [transport]);

  const create = useCallback(
    async (label: string) => {
      const trimmed = label.trim();
      if (!trimmed) {
        setError({ kind: "invalid", field: "label" });
        return;
      }
      setStatus("working");
      setError(null);
      try {
        // The secret lives in this state and nowhere else: it is shown once
        // and no later read can return it.
        setCreated(await transport.create({ label: trimmed }));
        await load();
      } catch (cause) {
        setError(errorOf(cause));
        setStatus("ready");
      }
    },
    [transport, load],
  );

  const revoke = useCallback(
    async (id: string) => {
      setStatus("working");
      setError(null);
      try {
        await transport.revoke(id);
        await load();
      } catch (cause) {
        setError(errorOf(cause));
        setStatus("ready");
      }
    },
    [transport, load],
  );

  return {
    keys,
    status,
    error,
    created,
    create,
    revoke,
    dismissCreated: useCallback(() => setCreated(null), []),
  };
}
