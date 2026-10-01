import { useEffect, useState } from "react";

import { hostOf, type FindSources, type FoundSource } from "./writing";

export interface SourcesPanelLabels {
  title: string;
  searching: string;
  empty: string;
  open: string;
  insert: string;
  close: string;
}

export const defaultSourcesPanelLabels: SourcesPanelLabels = {
  title: "Sources pour ce passage",
  searching: "Recherche…",
  empty: "Aucune source trouvée.",
  open: "Ouvrir",
  insert: "Insérer",
  close: "Fermer",
};

export interface SourcesPanelProps {
  passage: string;
  findSources: FindSources;
  onInsert: (source: FoundSource) => void;
  onClose: () => void;
  /** Extra per-source actions a host adds beside insert. */
  renderActions?: (source: FoundSource) => React.ReactNode;
  labels?: SourcesPanelLabels;
  formatError?: (error: unknown) => string;
}

const errorMessage = (error: unknown) => (error instanceof Error ? error.message : String(error));

export function SourcesPanel({
  passage,
  findSources,
  onInsert,
  onClose,
  renderActions,
  labels = defaultSourcesPanelLabels,
  formatError = errorMessage,
}: SourcesPanelProps) {
  const [sources, setSources] = useState<FoundSource[]>([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let current = true;
    setBusy(true);
    setError(null);
    findSources(passage)
      .then((found) => current && setSources(found))
      .catch((err: unknown) => current && setError(formatError(err)))
      .finally(() => current && setBusy(false));
    return () => {
      current = false;
    };
  }, [passage, findSources, formatError]);

  return (
    <div className="lalt-dialog" role="dialog" aria-modal="true" aria-label={labels.title}>
      <div className="lalt-dialog__panel">
        <div className="lalt-dialog__header">
          <h2 className="lalt-dialog__title">{labels.title}</h2>
          <button type="button" className="lalt-dialog__close" aria-label={labels.close} onClick={onClose}>
            ×
          </button>
        </div>

        <blockquote className="lalt-dialog__passage">{passage}</blockquote>

        {busy && <p className="lalt-dialog__status">{labels.searching}</p>}
        {error && <p className="lalt-dialog__error" role="alert">{error}</p>}
        {!busy && !error && sources.length === 0 && <p className="lalt-dialog__status">{labels.empty}</p>}

        <ul className="lalt-sources">
          {sources.map((source) => (
            <li key={source.url} className="lalt-sources__item">
              <p className="lalt-sources__title">{source.title || source.url}</p>
              {source.excerpt && <p className="lalt-sources__excerpt">{source.excerpt}</p>}
              <div className="lalt-sources__footer">
                <span className="lalt-sources__host">{hostOf(source.url)}</span>
                <a className="lalt-dialog__chip" href={source.url} target="_blank" rel="noopener noreferrer">
                  {labels.open}
                </a>
                <button type="button" className="lalt-dialog__chip" onClick={() => onInsert(source)}>
                  {labels.insert}
                </button>
                {renderActions?.(source)}
              </div>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
