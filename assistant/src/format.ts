export function relativeTime(iso: string | undefined, now = Date.now()): string {
  if (!iso) return "";
  const minutes = Math.round((now - new Date(iso).getTime()) / 60000);
  if (minutes < 1) return "à l'instant";
  if (minutes < 60) return `il y a ${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `il y a ${hours} heure${hours > 1 ? "s" : ""}`;
  const days = Math.round(hours / 24);
  return `il y a ${days} jour${days > 1 ? "s" : ""}`;
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} o`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} Ko`;
  return `${(bytes / (1024 * 1024)).toFixed(1).replace(/\.0$/, "")} Mo`;
}

const SCREEN_TEXT_LIMIT = 12000;

export function readMainScreen(): { path: string; title: string; text: string } | undefined {
  if (typeof document === "undefined") return undefined;
  const main = document.querySelector("main");
  const text = (main?.innerText ?? "").replace(/\n{3,}/g, "\n\n").trim().slice(0, SCREEN_TEXT_LIMIT);
  return { path: window.location.pathname + window.location.search, title: document.title, text };
}

export function errorMessage(error: unknown, fallback: string): string {
  const e = error as { message?: string } | null;
  return e?.message || fallback;
}
