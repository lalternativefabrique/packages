import type { Editor } from "@tiptap/core";

export interface DictationEnvironment {
  hasGetUserMedia: boolean;
  hasMediaRecorder: boolean;
}

export function isDictationSupported(env: DictationEnvironment = browserEnvironment()): boolean {
  return env.hasGetUserMedia && env.hasMediaRecorder;
}

function browserEnvironment(): DictationEnvironment {
  return {
    hasGetUserMedia: typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia,
    hasMediaRecorder: typeof MediaRecorder !== "undefined",
  };
}

// Safari records only audio/mp4 and Firefox prefers ogg; asking for a type the
// browser lacks makes the MediaRecorder constructor throw.
const MIME_CANDIDATES = [
  "audio/webm;codecs=opus",
  "audio/webm",
  "audio/ogg;codecs=opus",
  "audio/ogg",
  "audio/mp4",
];

export function pickMimeType(
  isTypeSupported: ((type: string) => boolean) | undefined = typeof MediaRecorder === "undefined"
    ? undefined
    : (type) => MediaRecorder.isTypeSupported(type),
): string | undefined {
  if (!isTypeSupported) return undefined;
  return MIME_CANDIDATES.find((type) => isTypeSupported(type));
}

export function spacedTranscript(preceding: string, transcript: string): string {
  const text = transcript.trim();
  if (text === "") return "";
  return preceding === "" || /\s$/.test(preceding) ? text : ` ${text}`;
}

export function insertTranscript(editor: Editor, transcript: string): boolean {
  const { from } = editor.state.selection;
  const preceding = editor.state.doc.textBetween(Math.max(0, from - 1), from, "\n", " ");
  const text = spacedTranscript(preceding, transcript);
  if (text === "") return false;
  return editor.chain().focus().insertContent(text).run();
}
