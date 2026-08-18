import { useEffect, useState } from "react";
import type { Editor } from "@tiptap/react";

import type { SelectionRect } from "./placement";

export interface SelectionInfo {
  text: string;
  rect: SelectionRect;
}

// Whether the on-screen keyboard is what stands between a selection and the
// toolbar acting on it. Touch without a fine pointer: a laptop with a
// touchscreen has a real keyboard and must keep its focus.
function keyboardIsInTheWay(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(hover: none) and (pointer: coarse)").matches;
}

/**
 * useSelectionToolbar reports the current non-empty selection of an editor.
 *
 * Returns null whenever there is nothing to act on, so the caller renders the
 * toolbar only while it has a target.
 *
 * On a touch device the keyboard is dismissed as soon as a range is selected.
 * There is nothing to type while choosing a format, and the strip above the
 * keyboard belongs to the system — a toolbar left to fight for it is drawn
 * underneath. Out of the way, the toolbar sits by the text it acts on.
 */
export function useSelectionToolbar(editor: Editor | null): SelectionInfo | null {
  const [selection, setSelection] = useState<SelectionInfo | null>(null);

  useEffect(() => {
    if (!editor) return;

    const read = () => {
      const { state } = editor;
      const { from, to, empty } = state.selection;
      // Focus is not required while a range is held. On a phone the toolbar
      // only becomes usable once the keyboard is out of the way, and putting
      // it away blurs the editor — reading focus here would take the toolbar
      // down with the keyboard, which is the thing the author reached for.
      if (empty) {
        setSelection(null);
        return;
      }

      const text = state.doc.textBetween(from, to, " ").trim();
      if (text === "") {
        setSelection(null);
        return;
      }

      // Blurred once per selection, not on every update: re-blurring while
      // the author drags a handle would fight iOS for the selection.
      if (keyboardIsInTheWay() && editor.isFocused) {
        (editor.view.dom as HTMLElement).blur();
      }

      const start = editor.view.coordsAtPos(from);
      const end = editor.view.coordsAtPos(to);
      setSelection({
        text,
        rect: {
          x: (start.left + end.right) / 2,
          top: Math.min(start.top, end.top),
          bottom: Math.max(start.bottom, end.bottom),
        },
      });
    };

    read();
    editor.on("selectionUpdate", read);
    editor.on("blur", read);
    editor.on("focus", read);
    return () => {
      editor.off("selectionUpdate", read);
      editor.off("blur", read);
      editor.off("focus", read);
    };
  }, [editor]);

  return selection;
}
