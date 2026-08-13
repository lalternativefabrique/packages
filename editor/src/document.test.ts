import { describe, expect, it } from "vitest";

import { blocksFromDoc, docFromBlocks, type RichDoc } from "./document";

const doc: RichDoc = {
  type: "doc",
  content: [
    { type: "paragraph", content: [{ type: "text", text: "Le chapeau." }] },
    { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Un titre" }] },
    { type: "paragraph", content: [{ type: "text", text: "Un paragraphe." }] },
  ],
};

describe("blocksFromDoc", () => {
  it("reads one block per top-level node", () => {
    expect(blocksFromDoc(doc)).toEqual(["Le chapeau.", "Un titre", "Un paragraphe."]);
  });

  it("joins the text of a node split across marks", () => {
    const split: RichDoc = {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "du gras " },
            { type: "text", text: "et du normal" },
          ],
        },
      ],
    };

    expect(blocksFromDoc(split)).toEqual(["du gras et du normal"]);
  });

  it("survives an empty document", () => {
    expect(blocksFromDoc({ type: "doc" })).toEqual([]);
  });

  it("keeps an empty paragraph out of the diff", () => {
    const withBlank: RichDoc = {
      type: "doc",
      content: [{ type: "paragraph" }, { type: "paragraph", content: [{ type: "text", text: "ok" }] }],
    };

    expect(blocksFromDoc(withBlank)).toEqual(["ok"]);
  });
});

describe("docFromBlocks", () => {
  it("round-trips the text of a document", () => {
    expect(blocksFromDoc(docFromBlocks(blocksFromDoc(doc)))).toEqual(blocksFromDoc(doc));
  });

  it("preserves the heading level of a block it was given", () => {
    const rebuilt = docFromBlocks(["Un titre"], [{ type: "heading", level: 2 }]);

    expect(rebuilt.content?.[0]).toMatchObject({ type: "heading", attrs: { level: 2 } });
  });

  it("defaults an unknown block to a paragraph", () => {
    const rebuilt = docFromBlocks(["Du texte"]);

    expect(rebuilt.content?.[0]).toMatchObject({ type: "paragraph" });
  });

  it("never emits a node Tiptap renders as a stray blank line", () => {
    const rebuilt = docFromBlocks(["", "  ", "vrai"]);

    expect(rebuilt.content).toHaveLength(1);
  });
});
