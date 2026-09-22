import { useMemo, useState } from "react";
import { httpTransport } from "./transport";
import { useAppKeys } from "./use-app-keys";
import { defaultCopy } from "./copy";
import type { KeysCopy, KeysTransport } from "./types";
import {
  ALERT,
  BADGE,
  BUTTON_PRIMARY,
  BUTTON_ROW_DANGER,
  BUTTON_SECONDARY,
  INPUT,
  PAGE_TITLE,
  ROW,
  TABLE,
  TABLE_WRAP,
  TD,
  TD_NUM,
  TD_STATE,
  TH,
  THEAD,
} from "./styles";

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
    <section className={`space-y-6 ${className ?? ""}`}>
      <header>
        <h2 className={PAGE_TITLE}>{text.title}</h2>
        <p className="mt-2 max-w-prose text-sm text-muted-foreground">
          {text.description}
        </p>
      </header>

      <form
        className="flex gap-2"
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
          className={INPUT}
        />
        <button
          type="submit"
          disabled={busy || !label.trim()}
          className={BUTTON_PRIMARY}
        >
          {busy ? text.creating : text.create}
        </button>
      </form>

      {created && (
        <div
          role="alert"
          className="space-y-3 rounded-xl border border-emerald-500/25 bg-emerald-500/5 p-5"
        >
          <div>
            <p className="font-medium">{text.secretTitle}</p>
            <p className="mt-1 text-sm text-muted-foreground">
              {text.secretWarning}
            </p>
          </div>
          <code className="block break-all rounded-lg border bg-background px-3 py-2.5 font-mono text-xs leading-relaxed select-all">
            {created.secret}
          </code>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className={BUTTON_PRIMARY}
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
              className={BUTTON_SECONDARY}
              onClick={() => {
                setCopied(false);
                dismissCreated();
              }}
            >
              {text.dismiss}
            </button>
          </div>
        </div>
      )}

      {error && (
        <p role="alert" className={ALERT}>
          {text.errors[error.kind]}
        </p>
      )}

      {error && keys.length === 0 ? null : (
        <div className={TABLE_WRAP}>
          <table className={TABLE}>
            <thead className={THEAD}>
              <tr>
                <th scope="col" className={TH}>
                  {text.columnLabel}
                </th>
                <th scope="col" className={TH}>
                  {text.columnId}
                </th>
                <th scope="col" className={TH}>
                  {text.columnScopes}
                </th>
                <th scope="col" className={TH}>
                  {text.columnCreated}
                </th>
                <th scope="col" className={TH}>
                  {text.columnExpires}
                </th>
                <th scope="col" className={TH}>
                  <span className="sr-only">{text.revoke}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {status === "loading" ? (
                <tr>
                  <td colSpan={6} className={TD_STATE}>
                    …
                  </td>
                </tr>
              ) : keys.length === 0 ? (
                <tr>
                  <td colSpan={6} className={TD_STATE}>
                    {text.empty}
                  </td>
                </tr>
              ) : (
                keys.map((key) => (
                  <tr key={key.id} className={ROW}>
                    <td className={`${TD} font-medium`}>{key.label}</td>
                    <td className={TD}>
                      <code className="font-mono text-xs text-muted-foreground">
                        {key.id}
                      </code>
                    </td>
                    <td className={TD}>
                      {key.scopes.length === 0 ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {key.scopes.map((scope) => (
                            <span key={scope} className={BADGE}>
                              {scope}
                            </span>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className={TD_NUM}>{formatDate(key.createdAt)}</td>
                    <td className={TD_NUM}>
                      {key.expiresAt ? formatDate(key.expiresAt) : text.never}
                    </td>
                    <td className={`${TD} text-right`}>
                      <button
                        type="button"
                        disabled={busy}
                        className={BUTTON_ROW_DANGER}
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
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
