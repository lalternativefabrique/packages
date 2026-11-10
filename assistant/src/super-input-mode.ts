export type SuperInputMode = "prompt" | "slash" | "command" | "shell" | "noshell";

export type SuperInputBang = "shell" | "elsewhere" | "text";

export function superInputMode(text: string, bang: SuperInputBang): SuperInputMode {
  const lead = text.trimStart();
  if (lead.startsWith("!") && bang !== "text") return bang === "shell" ? "shell" : "noshell";
  if (/^\/\S*$/.test(lead)) return "slash";
  if (lead.startsWith("/")) return "command";
  return "prompt";
}

export function shellCommand(text: string): string {
  return text.trim().replace(/^!\s*/, "").trim();
}

export function matchCommands<T extends { name: string }>(text: string, commands: T[]): T[] {
  const query = /^\/(\S*)$/.exec(text.trimStart())?.[1];
  if (query === undefined) return [];
  const q = query.toLowerCase();
  return commands.filter((c) => c.name.toLowerCase().startsWith(q));
}

export const HISTORY_LIMIT = 100;

export function pushHistory(history: string[], entry: string, limit = HISTORY_LIMIT): string[] {
  const trimmed = entry.trim();
  if (!trimmed || history[history.length - 1] === trimmed) return history;
  return [...history, trimmed].slice(-limit);
}

export function stepHistory(length: number, index: number, direction: -1 | 1): number {
  return Math.max(0, Math.min(length, index + direction));
}

export function lastLine(output: string): string {
  const lines = output.trimEnd().split("\n");
  return lines[lines.length - 1] ?? "";
}
