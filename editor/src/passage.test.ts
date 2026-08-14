import { describe, expect, it } from "vitest";

import { expandToWords, splitRevision } from "./passage";

describe("expandToWords", () => {
  const text = "Reprendre les moyens de production réellement.";

  it("grows a selection that starts mid-word", () => {
    const from = text.indexOf("yens");

    const range = expandToWords(text, from, text.indexOf("production"));

    expect(text.slice(range.from, range.to)).toMatch(/^moyens/);
  });

  it("grows a selection that ends mid-word", () => {
    const from = text.indexOf("moyens");
    const to = text.indexOf("réellem") + "réellem".length;

    const range = expandToWords(text, from, to);

    expect(text.slice(range.from, range.to)).toMatch(/réellement\.?$/);
  });

  it("leaves a selection already on word boundaries alone", () => {
    const from = text.indexOf("moyens");
    const to = from + "moyens".length;

    expect(expandToWords(text, from, to)).toEqual({ from, to });
  });

  it("keeps accented letters inside the word", () => {
    const from = text.indexOf("réel");

    const range = expandToWords(text, from + 2, from + 4);

    expect(text.slice(range.from, range.to)).toBe("réellement");
  });

  it("survives a selection at the very end", () => {
    const range = expandToWords(text, text.length - 1, text.length);

    expect(range.to).toBeLessThanOrEqual(text.length);
  });

  it("returns an empty range unchanged", () => {
    expect(expandToWords(text, 5, 5)).toEqual({ from: 5, to: 5 });
  });
});

describe("splitRevision", () => {
  it("pairs paragraphs one to one", () => {
    const parts = splitRevision("Un.\n\nDeux.", "UN.\n\nDEUX.");

    expect(parts).toHaveLength(2);
    expect(parts[0]).toEqual({ before: "Un.", after: "UN." });
    expect(parts[1]).toEqual({ before: "Deux.", after: "DEUX." });
  });

  it("drops paragraphs the model left untouched", () => {
    // An unchanged paragraph is not a decision: showing it would make the
    // author answer for text nobody proposed to change.
    const parts = splitRevision("Un.\n\nDeux.", "Un.\n\nDEUX.");

    expect(parts).toHaveLength(1);
    expect(parts[0].after).toBe("DEUX.");
  });

  it("falls back to one decision when the counts differ", () => {
    // Splitting a 2→3 rewrite by position would pair unrelated paragraphs and
    // present a diff that never existed.
    const parts = splitRevision("Un.\n\nDeux.", "Un.\n\nDeux.\n\nTrois.");

    expect(parts).toHaveLength(1);
    expect(parts[0].before).toBe("Un.\n\nDeux.");
  });

  it("treats a single paragraph as one decision", () => {
    const parts = splitRevision("Un seul.", "Un seul, revu.");

    expect(parts).toEqual([{ before: "Un seul.", after: "Un seul, revu." }]);
  });

  it("reports nothing when the model changed nothing", () => {
    expect(splitRevision("Un.", "Un.")).toEqual([]);
  });

  it("ignores blank paragraphs from repeated newlines", () => {
    const parts = splitRevision("Un.\n\n\n\nDeux.", "UN.\n\nDEUX.");

    expect(parts).toHaveLength(2);
  });
});
