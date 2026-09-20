import { useMemo, useState } from "react";
import { httpTransport } from "./transport";
import { useAppKeys } from "./use-app-keys";
import { defaultCopy } from "./copy";
import type { KeysCopy, KeysTransport } from "./types";

export interface AppKeysProps {
  endpoint?: string;
  transport?: KeysTransport;
  copy?: Partial<KeysCopy>;
  className?: string;
}

function formatDate(value: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "—"
    : date.toLocaleDateString("fr-FR", {
        day: "numeric",
        month: "short",
        year: "numeric",
      });
}

export function AppKeys({
  endpoint = "/api/keys",
  transport,
  copy,
  className,
}: AppKeysProps) {
  const wire = useMemo(
    () => transport ?? httpTransport(endpoint),
    [transport, endpoint],
  );
  const text = useMemo(() => ({ ...defaultCopy, ...copy }), [copy]);
  const { keys, status, error, created, create, revoke, dismissCreated } =
    useAppKeys(wire);
  const [label, setLabel] = useState("");
  const [copied, setCopied] = useState(false);

  const busy = status === "working";

  const submit = async () => {
    await create(label);
    setLabel("");
  };

  return (
    <section className={className}>
      <h2>{text.title}</h2>
      <p>{text.description}</p>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <input
          value={label}
          onChange={(event) => setLabel(event.target.value)}
          placeholder={text.labelPlaceholder}
          aria-label={text.labelPlaceholder}
          disabled={busy}
        />
        <button type="submit" disabled={busy || !label.trim()}>
          {busy ? text.creating : text.create}
        </button>
      </form>

      {created && (
        <div role="alert">
          <p>{text.secretTitle}</p>
          <p>{text.secretWarning}</p>
          <code>{created.secret}</code>
          <button
            type="button"
            onClick={() => {
              void navigator.clipboard
                ?.writeText(created.secret)
                .then(() => setCopied(true))
                .catch(() => setCopied(false));
            }}
          >
            {copied ? text.copied : text.copy}
          </button>
          <button
            type="button"
            onClick={() => {
              setCopied(false);
              dismissCreated();
            }}
          >
            {text.dismiss}
          </button>
        </div>
      )}

      {error && <p role="alert">{text.errors[error.kind]}</p>}

      {status === "loading" ||
      (error && keys.length === 0) ? null : keys.length === 0 ? (
        <p>{text.empty}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th scope="col">{text.columnLabel}</th>
              <th scope="col">{text.columnId}</th>
              <th scope="col">{text.columnScopes}</th>
              <th scope="col">{text.columnCreated}</th>
              <th scope="col">{text.columnExpires}</th>
              <th scope="col">
                <span className="sr-only">{text.revoke}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {keys.map((key) => (
              <tr key={key.id}>
                <td>{key.label}</td>
                <td>
                  <code>{key.id}</code>
                </td>
                <td>{key.scopes.join(" ") || "—"}</td>
                <td>{formatDate(key.createdAt)}</td>
                <td>
                  {key.expiresAt ? formatDate(key.expiresAt) : text.never}
                </td>
                <td>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => {
                      if (window.confirm(text.confirmRevoke(key.label))) {
                        void revoke(key.id);
                      }
                    }}
                  >
                    {busy ? text.revoking : text.revoke}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
