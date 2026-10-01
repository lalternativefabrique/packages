import { describe, expect, it } from "vitest";

import { isDictationSupported, pickMimeType, spacedTranscript } from "./dictation";

describe("isDictationSupported", () => {
  it("needs both the microphone and the recorder", () => {
    expect(isDictationSupported({ hasGetUserMedia: true, hasMediaRecorder: false })).toBe(false);
    expect(isDictationSupported({ hasGetUserMedia: true, hasMediaRecorder: true })).toBe(true);
  });
});

describe("pickMimeType", () => {
  it("prefers opus in webm", () => {
    expect(pickMimeType(() => true)).toBe("audio/webm;codecs=opus");
  });

  it("falls back to mp4 where only Safari's format is recorded", () => {
    expect(pickMimeType((type) => type === "audio/mp4")).toBe("audio/mp4");
  });

  it("lets the recorder choose when nothing is known", () => {
    expect(pickMimeType(undefined)).toBeUndefined();
    expect(pickMimeType(() => false)).toBeUndefined();
  });
});

describe("spacedTranscript", () => {
  it("separates the transcript from the word before the caret", () => {
    expect(spacedTranscript("t", " bonjour ")).toBe(" bonjour");
  });

  it("adds nothing at the start of a block or after a space", () => {
    expect(spacedTranscript("", "bonjour")).toBe("bonjour");
    expect(spacedTranscript(" ", "bonjour")).toBe("bonjour");
  });

  it("returns nothing for an empty transcript", () => {
    expect(spacedTranscript("t", "  ")).toBe("");
  });
});
