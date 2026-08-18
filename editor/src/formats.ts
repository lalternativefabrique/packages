import type { Editor } from "@tiptap/react";

// Type-only: these commands are contributed by StarterKit's extensions, and
// without the reference TypeScript does not know they exist on a chain. No
// runtime import, so the host stays in charge of which extensions it mounts —
// blockFormats simply returns entries whose commands no-op if one is missing.
import type {} from "@tiptap/starter-kit";

import type { BlockFormat } from "./SelectionToolbar";

export interface FormatLabels {
  paragraph: string;
  heading1: string;
  heading2: string;
  heading3: string;
  orderedList: string;
  bulletList: string;
}

export const defaultFormatLabels: FormatLabels = {
  paragraph: "Texte",
  heading1: "Titre 1",
  heading2: "Titre 2",
  heading3: "Titre 3",
  orderedList: "Liste numérotée",
  bulletList: "Liste à puces",
};

/**
 * blockFormats is the set a writer reaches for on a selected passage.
 *
 * Labels are passed in rather than translated here: the package carries no
 * i18n runtime, so the host names things in whatever language it speaks.
 */
export function blockFormats(
  editor: Editor | null,
  labels: FormatLabels = defaultFormatLabels,
): BlockFormat[] {
  if (!editor) return [];

  // Shortcuts are the ones TipTap's own extensions bind, read from their
  // source rather than invented: heading binds Mod-Alt-<level>, the list
  // extensions bind Mod-Shift-7/8, and setParagraph binds nothing at all.
  // A label promising a shortcut that does nothing is worse than no label.
  const mod = isApple() ? "⌘" : "Ctrl";

  return [
    {
      id: "paragraph",
      label: labels.paragraph,
      isActive: editor.isActive("paragraph"),
      onSelect: () => editor.chain().focus().setParagraph().run(),
    },
    ...([1, 2, 3] as const).map((level) => ({
      id: `heading${level}`,
      label: labels[`heading${level}` as keyof FormatLabels],
      shortcut: `${mod}+Alt+${level}`,
      isActive: editor.isActive("heading", { level }),
      onSelect: () => editor.chain().focus().toggleHeading({ level }).run(),
    })),
    {
      id: "orderedList",
      label: labels.orderedList,
      shortcut: `${mod}+Shift+7`,
      isActive: editor.isActive("orderedList"),
      onSelect: () => editor.chain().focus().toggleOrderedList().run(),
    },
    {
      id: "bulletList",
      label: labels.bulletList,
      shortcut: `${mod}+Shift+8`,
      isActive: editor.isActive("bulletList"),
      onSelect: () => editor.chain().focus().toggleBulletList().run(),
    },
  ];
}

function isApple(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Mac|iPad|iPhone|iPod/.test(navigator.platform || navigator.userAgent);
}
