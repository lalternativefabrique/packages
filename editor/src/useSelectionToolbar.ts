import { useEffect, useState } from "react";
import type { Editor } from "@tiptap/react";

import type { SelectionRect } from "./placement";

export interface SelectionInfo {
  text: string;
  rect: SelectionRect;
}

/**
 * useSelectionToolbar reports the current non-empty selection of an editor.
 *
 * Returns null whenever there is nothing to act on, so the caller renders the
 * toolbar only while it has a target.
 */
export function useSelectionToolbar(editor: Editor | null): SelectionInfo | null {
  const [selection, setSelection] = useState<SelectionInfo | null>(null);

  useEffect(() => {
    if (!editor) return;

    const read = () => {
      const { state } = editor;
      const { from, to, empty } = state.selection;
      if (empty || !editor.isFocused) {
        setSelection(null);
        return;
      }

      const text = state.doc.textBetween(from, to, " ").trim();
      if (text === "") {
        setSelection(null);
        return;
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
