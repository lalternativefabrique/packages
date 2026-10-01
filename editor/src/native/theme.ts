export interface EditorToolbarTheme {
  surface: string;
  text: string;
  muted: string;
  border: string;
  pressed: string;
  accent: string;
  backdrop: string;
}

export const defaultEditorToolbarTheme: EditorToolbarTheme = {
  surface: "#ffffff",
  text: "#111827",
  muted: "rgba(17, 24, 39, 0.5)",
  border: "rgba(0, 0, 0, 0.1)",
  pressed: "rgba(0, 0, 0, 0.06)",
  accent: "#1d4ed8",
  backdrop: "rgba(0, 0, 0, 0.4)",
};
