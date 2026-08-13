export interface RichNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: RichNode[];
  text?: string;
}

export type RichDoc = RichNode;

export interface BlockShape {
  type: "paragraph" | "heading";
  level?: number;
}

/**
 * blocksFromDoc flattens a TipTap document to the text of its top-level nodes.
 *
 * Marks are dropped rather than encoded: the diff compares prose, and a bold
 * span that moved inside an otherwise identical sentence is not a change worth
 * asking the reviewer about.
 */
export function blocksFromDoc(doc: RichDoc): string[] {
  return (doc.content ?? []).map(textOf).filter((text) => text.trim() !== "");
}

/** shapesFromDoc records what each block was, so a revision can be rebuilt
 *  without turning every heading into a paragraph. */
export function shapesFromDoc(doc: RichDoc): BlockShape[] {
  return (doc.content ?? [])
    .filter((node) => textOf(node).trim() !== "")
    .map((node) =>
      node.type === "heading"
        ? { type: "heading" as const, level: Number(node.attrs?.level ?? 2) }
        : { type: "paragraph" as const },
    );
}

export function docFromBlocks(blocks: string[], shapes: BlockShape[] = []): RichDoc {
  const content: RichNode[] = [];
  blocks.forEach((block, i) => {
    const text = block.trim();
    if (text === "") return;

    const shape = shapes[i];
    if (shape?.type === "heading") {
      content.push({
        type: "heading",
        attrs: { level: shape.level ?? 2 },
        content: [{ type: "text", text }],
      });
      return;
    }
    content.push({ type: "paragraph", content: [{ type: "text", text }] });
  });
  return { type: "doc", content };
}

function textOf(node: RichNode): string {
  if (typeof node.text === "string") return node.text;
  return (node.content ?? []).map(textOf).join("");
}
