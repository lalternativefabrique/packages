// Mirrors @lalternative/admin's class strings so a key list reads as one of
// its tables; the `hsl(var(--…))` tokens come from the host, e.g. `.lalt-admin`.

export const PAGE_TITLE = "text-2xl font-semibold tracking-tight";

export const INPUT =
  "w-full max-w-xs rounded-lg border bg-background px-4 py-2.5 text-sm outline-none transition-colors " +
  "placeholder:text-muted-foreground/60 disabled:opacity-50 " +
  "focus-visible:border-foreground/40 focus-visible:ring-2 focus-visible:ring-foreground/15";

export const BUTTON_PRIMARY =
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-lg bg-foreground px-4 py-2.5 " +
  "text-sm font-medium text-background transition-opacity hover:opacity-90 disabled:opacity-50 " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30";

export const BUTTON_SECONDARY =
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-lg border bg-background px-4 py-2.5 " +
  "text-sm font-medium transition-colors hover:bg-muted disabled:opacity-50 " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30";

export const ALERT =
  "rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive";

export const TABLE_WRAP =
  "admin-paper overflow-x-auto rounded-xl border bg-card";

export const TABLE = "w-full text-left text-sm";

export const THEAD =
  "border-b text-xs uppercase tracking-wide text-muted-foreground";

export const TH = "px-4 py-3 font-medium whitespace-nowrap";

export const TD = "px-4 py-3";

export const TD_NUM =
  "px-4 py-3 whitespace-nowrap text-muted-foreground tabular-nums";

export const TD_STATE = "px-4 py-10 text-center text-muted-foreground";

export const ROW = "border-b transition-colors last:border-0 hover:bg-muted/40";

export const BADGE =
  "inline-flex items-center rounded-full border border-border bg-muted px-2 py-0.5 font-mono text-xs text-muted-foreground";

export const BUTTON_ROW_DANGER =
  "rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors " +
  "hover:bg-destructive/10 hover:text-destructive disabled:opacity-50";
