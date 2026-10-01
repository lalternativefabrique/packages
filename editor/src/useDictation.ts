import { useCallback, useEffect, useRef, useState } from "react";

import { isDictationSupported, pickMimeType } from "./dictation";

export type DictationState = "idle" | "recording" | "transcribing";

export interface UseDictationOptions {
  transcribe: (audio: Blob) => Promise<string>;
}

export interface UseDictation {
  state: DictationState;
  supported: boolean;
  error: Error | null;
  start: () => Promise<void>;
  /** Stops recording and resolves with the transcript, or null when nothing was heard. */
  stop: () => Promise<string | null>;
  toggle: () => Promise<string | null>;
}

export function useDictation({ transcribe }: UseDictationOptions): UseDictation {
  const [state, setState] = useState<DictationState>("idle");
  const [supported, setSupported] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const transcribeRef = useRef(transcribe);
  transcribeRef.current = transcribe;

  const release = useCallback(() => {
    stream.current?.getTracks().forEach((track) => track.stop());
    stream.current = null;
    recorder.current = null;
  }, []);

  useEffect(() => {
    setSupported(isDictationSupported());
    return release;
  }, [release]);

  const start = useCallback(async () => {
    if (recorder.current) return;
    if (!isDictationSupported()) {
      setError(new Error("dictation is not supported in this browser"));
      return;
    }
    setError(null);
    try {
      stream.current = await navigator.mediaDevices.getUserMedia({ audio: true });
      const mimeType = pickMimeType();
      const next = new MediaRecorder(stream.current, mimeType ? { mimeType } : undefined);
      next.start();
      recorder.current = next;
      setState("recording");
    } catch (err) {
      release();
      setError(err instanceof Error ? err : new Error(String(err)));
      setState("idle");
    }
  }, [release]);

  const stop = useCallback(async (): Promise<string | null> => {
    const active = recorder.current;
    if (!active) return null;

    const audio = await new Promise<Blob>((resolve, reject) => {
      const chunks: Blob[] = [];
      active.ondataavailable = (event) => {
        if (event.data.size > 0) chunks.push(event.data);
      };
      active.onerror = () => reject(new Error("recording failed"));
      active.onstop = () => resolve(new Blob(chunks, { type: active.mimeType || "audio/webm" }));
      active.stop();
    }).finally(release);

    if (audio.size === 0) {
      setState("idle");
      return null;
    }

    setState("transcribing");
    try {
      const text = (await transcribeRef.current(audio)).trim();
      return text === "" ? null : text;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      return null;
    } finally {
      setState("idle");
    }
  }, [release]);

  const toggle = useCallback(async () => {
    if (recorder.current) return stop();
    await start();
    return null;
  }, [start, stop]);

  return { state, supported, error, start, stop, toggle };
}
