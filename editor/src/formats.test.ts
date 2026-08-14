import { describe, expect, it } from "vitest";

import { blockFormats } from "./formats";

const editor = {
  isActive: () => false,
  chain: () => ({ focus: () => ({ setParagraph: () => ({ run: () => true }) }) }),
} as never;

describe("blockFormats", () => {
  it("offers heading levels 1 to 3", () => {
    const ids = blockFormats(editor).map((f) => f.id);

    expect(ids).toContain("heading1");
    expect(ids).toContain("heading2");
    expect(ids).toContain("heading3");
  });

  it("promises no shortcut for paragraph, which TipTap binds to nothing", () => {
    const paragraph = blockFormats(editor).find((f) => f.id === "paragraph");

    expect(paragraph?.shortcut).toBeUndefined();
  });

  it("labels headings with the shortcut the heading extension actually binds", () => {
    const heading2 = blockFormats(editor).find((f) => f.id === "heading2");

    expect(heading2?.shortcut).toMatch(/Alt\+2$/);
  });

  it("labels lists with Shift+7 and Shift+8, not the Alt run of the headings", () => {
    const formats = blockFormats(editor);

    expect(formats.find((f) => f.id === "orderedList")?.shortcut).toMatch(/Shift\+7$/);
    expect(formats.find((f) => f.id === "bulletList")?.shortcut).toMatch(/Shift\+8$/);
  });

  it("returns nothing without an editor rather than throwing at render time", () => {
    expect(blockFormats(null)).toEqual([]);
  });
});
