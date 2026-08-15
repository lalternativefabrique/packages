import type { Editor } from "@tiptap/react";
import type { Range } from "@tiptap/core";

// Type-only, as in formats.ts: StarterKit contributes these commands to a
// chain, and without the reference TypeScript does not know they exist. The
// host stays in charge of which extensions it actually mounts.
import type {} from "@tiptap/starter-kit";

/**
 * SlashItem is one entry of the `/` menu.
 *
 * icon is a node rather than a component type: the package carries no icon
 * library, so the host renders whichever one it already ships.
 */
export interface SlashItem {
  id: string;
  title: string;
  description?: string;
  shortcut?: string;
  icon?: React.ReactNode;
  command: (args: { editor: Editor; range: Range }) => void;
}

export interface SlashLabels {
  heading1: string;
  heading2: string;
  bulletList: string;
  orderedList: string;
  blockquote: string;
  codeBlock: string;
  horizontalRule: string;
}

export const defaultSlashLabels: SlashLabels = {
  heading1: "Titre 1",
  heading2: "Titre 2",
  bulletList: "Liste à puces",
  orderedList: "Liste numérotée",
  blockquote: "Citation",
  codeBlock: "Bloc de code",
  horizontalRule: "Séparateur",
};

export interface SlashDescriptions {
  heading1?: string;
  heading2?: string;
  bulletList?: string;
  orderedList?: string;
  blockquote?: string;
  codeBlock?: string;
  horizontalRule?: string;
}

export interface DefaultSlashItemsOptions {
  labels?: SlashLabels;
  descriptions?: SlashDescriptions;
  icons?: Partial<Record<DefaultSlashId, React.ReactNode>>;
}

export type DefaultSlashId =
  | "heading1"
  | "heading2"
  | "bulletList"
  | "orderedList"
  | "blockquote"
  | "codeBlock"
  | "horizontalRule";

/**
 * defaultSlashItems is the block set every writing surface shares.
 *
 * Returned as data rather than rendered behind flags: a host adds, removes or
 * reorders entries by composing the array, so a surface that needs one more
 * command does not push another boolean into this signature.
 */
export function defaultSlashItems({
  labels = defaultSlashLabels,
  descriptions = {},
  icons = {},
}: DefaultSlashItemsOptions = {}): SlashItem[] {
  const item = (
    id: DefaultSlashId,
    shortcut: string,
    run: (editor: Editor) => void,
  ): SlashItem => ({
    id,
    title: labels[id],
    description: descriptions[id],
    shortcut,
    icon: icons[id],
    command: ({ editor, range }) => {
      editor.chain().focus().deleteRange(range).run();
      run(editor);
    },
  });

  return [
    item("heading1", "#", (editor) =>
      editor.chain().focus().setNode("heading", { level: 1 }).run(),
    ),
    item("heading2", "##", (editor) =>
      editor.chain().focus().setNode("heading", { level: 2 }).run(),
    ),
    item("bulletList", "-", (editor) =>
      editor.chain().focus().toggleBulletList().run(),
    ),
    item("orderedList", "1.", (editor) =>
      editor.chain().focus().toggleOrderedList().run(),
    ),
    item("blockquote", ">", (editor) =>
      editor.chain().focus().toggleBlockquote().run(),
    ),
    item("codeBlock", "```", (editor) =>
      editor.chain().focus().toggleCodeBlock().run(),
    ),
    item("horizontalRule", "---", (editor) =>
      editor.chain().focus().setHorizontalRule().run(),
    ),
  ];
}

/** filterSlashItems matches the query against titles the way the menu does. */
export function filterSlashItems(items: SlashItem[], query: string): SlashItem[] {
  const q = query.trim().toLowerCase();
  if (q === "") return items;
  return items.filter((item) => item.title.toLowerCase().includes(q));
}
