import type { RichDoc, RichNode } from "./document";

interface Mark {
  type?: string;
  attrs?: Record<string, unknown>;
}

/**
 * docToMarkdown renders a TipTap document as the markdown a blog accepts.
 *
 * Written here rather than taken from a library: the editor only ever produces
 * the handful of nodes StarterKit gives it, and a general markdown serializer
 * brings a dependency and its escaping rules for output a human is about to
 * paste into their own site.
 */
export function docToMarkdown(doc: RichDoc): string {
  return blocks(doc.content ?? [])
    .filter((b) => b !== "")
    .join("\n\n");
}

function blocks(nodes: RichNode[]): string[] {
  return nodes.map(block);
}

function block(node: RichNode): string {
  switch (node.type) {
    case "heading": {
      const level = Number(node.attrs?.level ?? 2);
      return `${"#".repeat(Math.min(6, Math.max(1, level)))} ${inline(node.content)}`;
    }
    case "blockquote":
      return blocks(node.content ?? [])
        .filter((b) => b !== "")
        .map((b) =>
          b
            .split("\n")
            .map((line) => `> ${line}`)
            .join("\n"),
        )
        .join("\n>\n");
    case "codeBlock": {
      const language = String(node.attrs?.language ?? "");
      return `\`\`\`${language}\n${inline(node.content)}\n\`\`\``;
    }
    case "bulletList":
      return listItems(node).map((item) => `- ${item}`).join("\n");
    case "orderedList":
      return listItems(node)
        .map((item, i) => `${i + 1}. ${item}`)
        .join("\n");
    case "horizontalRule":
      return "---";
    default:
      return inline(node.content);
  }
}

function listItems(list: RichNode): string[] {
  return (list.content ?? []).map((item) =>
    blocks(item.content ?? [])
      .filter((b) => b !== "")
      // Continuation lines are indented so the item stays one item.
      .join("\n\n")
      .split("\n")
      .join("\n  "),
  );
}

function inline(nodes: RichNode[] | undefined): string {
  return (nodes ?? []).map(text).join("");
}

function text(node: RichNode): string {
  if (node.type === "hardBreak") return "\n";
  if (typeof node.text !== "string") return inline(node.content);

  let out = node.text;
  // Innermost first, so bold inside a link wraps the text and not the URL.
  for (const mark of ((node.marks as Mark[] | undefined) ?? []).slice().reverse()) {
    switch (mark.type) {
      case "bold":
        out = `**${out}**`;
        break;
      case "italic":
        out = `*${out}*`;
        break;
      case "strike":
        out = `~~${out}~~`;
        break;
      case "code":
        out = `\`${out}\``;
        break;
      case "link":
        out = `[${out}](${String(mark.attrs?.href ?? "")})`;
        break;
    }
  }
  return out;
}
