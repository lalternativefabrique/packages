import { useEffect } from "react";

import { useDictation } from "./useDictation";

export interface DictationLabels {
  start: string;
  stop: string;
  transcribing: string;
  unsupported: string;
}

export const defaultDictationLabels: DictationLabels = {
  start: "Dicter",
  stop: "Arrêter la dictée",
  transcribing: "Transcription…",
  unsupported: "La dictée n’est pas disponible dans ce navigateur",
};

export interface DictationButtonProps {
  transcribe: (audio: Blob) => Promise<string>;
  onTranscript: (text: string) => void;
  onError?: (error: Error) => void;
  labels?: DictationLabels;
  icon?: React.ReactNode;
  disabled?: boolean;
  className?: string;
}

export function DictationButton({
  transcribe,
  onTranscript,
  onError,
  labels = defaultDictationLabels,
  icon = <span aria-hidden>●</span>,
  disabled = false,
  className,
}: DictationButtonProps) {
  const { state, supported, error, toggle } = useDictation({ transcribe });

  useEffect(() => {
    if (error) onError?.(error);
  }, [error, onError]);

  const label = !supported
    ? labels.unsupported
    : state === "recording"
      ? labels.stop
      : state === "transcribing"
        ? labels.transcribing
        : labels.start;

  return (
    <button
      type="button"
      className={["lalt-dictation", className].filter(Boolean).join(" ")}
      data-state={state}
      aria-pressed={state === "recording"}
      aria-label={label}
      title={error?.message ?? label}
      disabled={disabled || !supported || state === "transcribing"}
      onMouseDown={(e) => e.preventDefault()}
      onClick={async () => {
        const text = await toggle();
        if (text) onTranscript(text);
      }}
    >
      {icon}
    </button>
  );
}
