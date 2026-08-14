import { describe, expect, it } from "vitest";

import { docToMarkdown } from "./markdown";
import type { RichDoc } from "./document";

const doc = (content: RichDoc["content"]): RichDoc => ({ type: "doc", content });

describe("docToMarkdown", () => {
  it("writes a heading with the hashes of its level", () => {
    const md = docToMarkdown(
      doc([{ type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Un titre" }] }]),
    );

    expect(md).toBe("## Un titre");
  });

  it("separates blocks with a blank line", () => {
    const md = docToMarkdown(
      doc([
        { type: "paragraph", content: [{ type: "text", text: "Un." }] },
        { type: "paragraph", content: [{ type: "text", text: "Deux." }] },
      ]),
    );

    expect(md).toBe("Un.\n\nDeux.");
  });

  it("marks bold and italic", () => {
    const md = docToMarkdown(
      doc([
        {
          type: "paragraph",
          content: [
            { type: "text", text: "du " },
            { type: "text", text: "gras", marks: [{ type: "bold" }] },
            { type: "text", text: " et de l'" },
            { type: "text", text: "italique", marks: [{ type: "italic" }] },
          ],
        },
      ]),
    );

    expect(md).toBe("du **gras** et de l'*italique*");
  });

  it("writes a link as its text and href", () => {
    const md = docToMarkdown(
      doc([
        {
          type: "paragraph",
          content: [
            {
              type: "text",
              text: "le blog",
              marks: [{ type: "link", attrs: { href: "https://example.org" } }],
            },
          ],
        },
      ]),
    );

    expect(md).toBe("[le blog](https://example.org)");
  });

  it("numbers an ordered list and bullets an unordered one", () => {
    const item = (text: string) => ({
      type: "listItem",
      content: [{ type: "paragraph", content: [{ type: "text", text }] }],
    });

    expect(docToMarkdown(doc([{ type: "bulletList", content: [item("un"), item("deux")] }]))).toBe(
      "- un\n- deux",
    );
    expect(docToMarkdown(doc([{ type: "orderedList", content: [item("un"), item("deux")] }]))).toBe(
      "1. un\n2. deux",
    );
  });

  it("prefixes a blockquote", () => {
    const md = docToMarkdown(
      doc([
        {
          type: "blockquote",
          content: [{ type: "paragraph", content: [{ type: "text", text: "Une citation." }] }],
        },
      ]),
    );

    expect(md).toBe("> Une citation.");
  });

  it("fences a code block with its language", () => {
    const md = docToMarkdown(
      doc([
        {
          type: "codeBlock",
          attrs: { language: "go" },
          content: [{ type: "text", text: "func main() {}" }],
        },
      ]),
    );

    expect(md).toBe("```go\nfunc main() {}\n```");
  });

  it("drops empty paragraphs rather than leaving blank runs", () => {
    const md = docToMarkdown(
      doc([
        { type: "paragraph", content: [{ type: "text", text: "Un." }] },
        { type: "paragraph" },
        { type: "paragraph", content: [{ type: "text", text: "Deux." }] },
      ]),
    );

    expect(md).toBe("Un.\n\nDeux.");
  });

  it("returns an empty string for an empty document", () => {
    expect(docToMarkdown({ type: "doc" })).toBe("");
  });

  it("keeps a hard break inside a paragraph", () => {
    const md = docToMarkdown(
      doc([
        {
          type: "paragraph",
          content: [
            { type: "text", text: "Une ligne" },
            { type: "hardBreak" },
            { type: "text", text: "et la suite" },
          ],
        },
      ]),
    );

    expect(md).toBe("Une ligne\net la suite");
  });
});
