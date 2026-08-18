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

  // focus() is what makes a command apply to the selection rather than to
  // nothing — but on a phone the toolbar is only reachable because the
  // keyboard was dismissed, and focusing calls it straight back. The
  // scrollIntoView: false variant restores the range without the editor
  // asking for the keyboard again.
  const act = (run: () => boolean) => {
    const focused = editor.isFocused;
    if (focused) return run();
    editor.commands.focus(undefined, { scrollIntoView: false });
    const applied = run();
    (editor.view.dom as HTMLElement).blur();
    return applied;
  };

  return [
    {
      id: "paragraph",
      label: labels.paragraph,
      isActive: editor.isActive("paragraph"),
      onSelect: () => act(() => editor.chain().setParagraph().run()),
    },
    ...([1, 2, 3] as const).map((level) => ({
      id: `heading${level}`,
      label: labels[`heading${level}` as keyof FormatLabels],
      shortcut: `${mod}+Alt+${level}`,
      isActive: editor.isActive("heading", { level }),
      onSelect: () => act(() => editor.chain().toggleHeading({ level }).run()),
    })),
    {
      id: "orderedList",
      label: labels.orderedList,
      shortcut: `${mod}+Shift+7`,
      isActive: editor.isActive("orderedList"),
      onSelect: () => act(() => editor.chain().toggleOrderedList().run()),
    },
    {
      id: "bulletList",
      label: labels.bulletList,
      shortcut: `${mod}+Shift+8`,
      isActive: editor.isActive("bulletList"),
      onSelect: () => act(() => editor.chain().toggleBulletList().run()),
    },
  ];
}

function isApple(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Mac|iPad|iPhone|iPod/.test(navigator.platform || navigator.userAgent);
}
