import { useState } from "react";

import { defaultRevisionPresets, normalizeInstruction, proposalChanges, type Revise } from "./writing";

export interface RevisionPromptLabels {
  title: string;
  instruction: string;
  placeholder: string;
  submit: string;
  busy: string;
  proposal: string;
  unchanged: string;
  accept: string;
  reject: string;
  close: string;
}

export const defaultRevisionPromptLabels: RevisionPromptLabels = {
  title: "Retravailler le passage",
  instruction: "Consigne",
  placeholder: "Ex. : ajoute un exemple concret",
  submit: "Retravailler",
  busy: "Réécriture…",
  proposal: "Proposition",
  unchanged: "Le modèle n’a rien changé.",
  accept: "Remplacer",
  reject: "Garder l’original",
  close: "Fermer",
};

export interface RevisionPromptProps {
  passage: string;
  revise: Revise;
  /** Called with the proposal once the author accepts it; the prompt never writes on its own. */
  onAccept: (proposal: string) => void;
  onClose: () => void;
  presets?: readonly string[];
  labels?: RevisionPromptLabels;
  formatError?: (error: unknown) => string;
}

const errorMessage = (error: unknown) => (error instanceof Error ? error.message : String(error));

export function RevisionPrompt({
  passage,
  revise,
  onAccept,
  onClose,
  presets = defaultRevisionPresets,
  labels = defaultRevisionPromptLabels,
  formatError = errorMessage,
}: RevisionPromptProps) {
  const [instruction, setInstruction] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [proposal, setProposal] = useState<string | null>(null);

  const send = async (value: string) => {
    const normalized = normalizeInstruction(value);
    if (!normalized || busy) return;
    setBusy(true);
    setError(null);
    try {
      setProposal(await revise(passage, normalized));
    } catch (err) {
      setError(formatError(err));
    } finally {
      setBusy(false);
    }
  };

  const unchanged = proposal !== null && proposalChanges(passage, proposal).length === 0;

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

        {proposal !== null ? (
          <>
            <p className="lalt-dialog__caption">{labels.proposal}</p>
            <div className="lalt-dialog__proposal">{unchanged ? labels.unchanged : proposal}</div>
            <div className="lalt-dialog__actions">
              <button
                type="button"
                className="lalt-dialog__primary"
                disabled={unchanged}
                onClick={() => onAccept(proposal)}
              >
                {labels.accept}
              </button>
              <button type="button" className="lalt-dialog__secondary" onClick={onClose}>
                {labels.reject}
              </button>
            </div>
          </>
        ) : (
          <>
            <label className="lalt-dialog__caption" htmlFor="lalt-revision-instruction">
              {labels.instruction}
            </label>
            <textarea
              id="lalt-revision-instruction"
              className="lalt-dialog__input"
              autoFocus
              rows={2}
              value={instruction}
              placeholder={labels.placeholder}
              onChange={(e) => setInstruction(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void send(instruction);
                }
              }}
            />
            <div className="lalt-dialog__presets">
              {presets.map((preset) => (
                <button
                  key={preset}
                  type="button"
                  className="lalt-dialog__chip"
                  disabled={busy}
                  onClick={() => void send(preset)}
                >
                  {preset}
                </button>
              ))}
            </div>
            {error && <p className="lalt-dialog__error" role="alert">{error}</p>}
            <button
              type="button"
              className="lalt-dialog__primary"
              disabled={busy || normalizeInstruction(instruction) === null}
              onClick={() => void send(instruction)}
            >
              {busy ? labels.busy : labels.submit}
            </button>
          </>
        )}
      </div>
    </div>
  );
}
