export type FormatActionId =
  | "bold"
  | "italic"
  | "underline"
  | "strike"
  | "h1"
  | "h2"
  | "h3"
  | "h4"
  | "bullet"
  | "ordered";

export interface FormatAction {
  id: FormatActionId;
  label: string;
  glyph: string;
  /** The markdown the action writes, shown as its shortcut. */
  shortcut: string;
  prefix?: string;
  wrap?: string;
}

export type NativeFormatLabels = Record<FormatActionId, string>;

export const defaultNativeFormatLabels: NativeFormatLabels = {
  bold: "Gras",
  italic: "Italique",
  underline: "Souligné",
  strike: "Barré",
  h1: "Titre 1",
  h2: "Titre 2",
  h3: "Titre 3",
  h4: "Titre 4",
  bullet: "Liste à puces",
  ordered: "Liste numérotée",
};

export function formatActions(labels: NativeFormatLabels = defaultNativeFormatLabels): FormatAction[] {
  const action = (id: FormatActionId, glyph: string, shortcut: string, edit: Pick<FormatAction, "prefix" | "wrap">) => ({
    id,
    label: labels[id],
    glyph,
    shortcut,
    ...edit,
  });
  return [
    action("bold", "B", "**", { wrap: "**" }),
    action("italic", "I", "*", { wrap: "*" }),
    action("underline", "U", "++", { wrap: "++" }),
    action("strike", "S", "~~", { wrap: "~~" }),
    action("h1", "H1", "#", { prefix: "# " }),
    action("h2", "H2", "##", { prefix: "## " }),
    action("h3", "H3", "###", { prefix: "### " }),
    action("h4", "H4", "####", { prefix: "#### " }),
    action("bullet", "•—", "-", { prefix: "- " }),
    action("ordered", "1.", "1.", { prefix: "1. " }),
  ];
}

export interface TextSelection {
  start: number;
  end: number;
}

export interface FormattedText {
  text: string;
  selection: TextSelection;
}

/** applyFormat writes an action into a plain-text field, for hosts without a rich editor. */
export function applyFormat(text: string, selection: TextSelection, action: FormatAction): FormattedText {
  const { start, end } = selection;
  if (action.prefix) {
    const lineStart = text.lastIndexOf("\n", start - 1) + 1;
    const shift = action.prefix.length;
    return {
      text: text.slice(0, lineStart) + action.prefix + text.slice(lineStart),
      selection: { start: start + shift, end: end + shift },
    };
  }
  const wrap = action.wrap ?? "";
  return {
    text: text.slice(0, start) + wrap + text.slice(start, end) + wrap + text.slice(end),
    selection: { start: start + wrap.length, end: end + wrap.length },
  };
}
