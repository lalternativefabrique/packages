import { useCallback, useEffect, useRef, useState } from "react";
import type { AssistantLabels, AssistantTranscribe } from "./types";

const MIME_TYPES = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];
const BAR_DELAYS_MS = [0, 160, 320, 80, 240];

export type DictationState = "idle" | "recording" | "transcribing";

export function useDictation(transcribe: AssistantTranscribe | undefined, onText: (text: string) => void, labels: AssistantLabels, language = "fr") {
  const [state, setState] = useState<DictationState>("idle");
  const [error, setError] = useState("");
  const recorder = useRef<MediaRecorder | null>(null);
  const cancelled = useRef(false);
  const supported =
    !!transcribe && typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== "undefined";

  useEffect(() => () => recorder.current?.stream.getTracks().forEach((t) => t.stop()), []);

  const start = useCallback(async () => {
    if (recorder.current || !transcribe) return;
    setError("");
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (err) {
      setError(err instanceof Error && err.name === "NotAllowedError" ? labels.micDenied : labels.micMissing);
      return;
    }
    const mime = MIME_TYPES.find((m) => MediaRecorder.isTypeSupported(m)) ?? "";
    const rec = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
    const chunks: Blob[] = [];
    rec.ondataavailable = (e) => {
      if (e.data.size > 0) chunks.push(e.data);
    };
    rec.onstop = async () => {
      stream.getTracks().forEach((t) => t.stop());
      recorder.current = null;
      const blob = new Blob(chunks, { type: rec.mimeType || mime || "audio/webm" });
      if (cancelled.current || blob.size === 0) {
        cancelled.current = false;
        setState("idle");
        return;
      }
      setState("transcribing");
      try {
        const out = await transcribe(blob, language);
        if (typeof out === "string") {
          if (out.trim()) onText(out.trim());
        } else setError(out.notice);
      } catch (err) {
        setError(err instanceof Error && err.message ? err.message : labels.transcribeFailed);
      } finally {
        setState("idle");
      }
    };
    recorder.current = rec;
    cancelled.current = false;
    rec.start();
    setState("recording");
  }, [transcribe, onText, labels, language]);

  const stop = useCallback(() => recorder.current?.stop(), []);
  const cancel = useCallback(() => {
    if (!recorder.current) return false;
    cancelled.current = true;
    recorder.current.stop();
    return true;
  }, []);

  return { state, error, supported, start, stop, cancel, clearError: () => setError("") };
}

function useElapsed() {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const at = Date.now();
    const t = setInterval(() => setSeconds(Math.floor((Date.now() - at) / 1000)), 500);
    return () => clearInterval(t);
  }, []);
  return seconds;
}

export function MicBars() {
  const s = useElapsed();
  return (
    <span className="lalt-assistant-mic-bars" role="status">
      <span aria-hidden="true" className="lalt-assistant-mic-bars-lines">
        {BAR_DELAYS_MS.map((delay, i) => (
          <span key={i} style={{ animationDelay: `${delay}ms` }} />
        ))}
      </span>
      <span className="lalt-assistant-mic-clock">{`${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`}</span>
    </span>
  );
}
