/**
 * Shared class strings for the admin surface.
 *
 * Tokens come from `admin.css`, scoped to `.lalt-admin` and owned by this
 * package rather than read from the host — so the back-office renders the same
 * in every app, including one that defines no theme of its own.
 *
 * The primary action and focus rings are plain black (white on dark),
 * deliberately NOT `--primary`, so brand colour never competes with the
 * emerald/violet the tables already use to carry state.
 *
 * Cards and tables use `admin-paper`: a sheet one step warmer than the ground,
 * lifted by a shadow instead of closed by a hard border.
 */

/**
 * Neutral text input. `bg-background` sits a step behind the paper surface,
 * which is what makes the field legible as an input without a heavy border.
 */
export const INPUT =
  "w-full rounded-lg border bg-background px-4 py-2.5 text-sm outline-none transition-colors " +
  "placeholder:text-muted-foreground/60 " +
  "focus-visible:border-foreground/40 focus-visible:ring-2 focus-visible:ring-foreground/15"

/** Neutral primary button: black on light, white on dark. */
export const BUTTON_PRIMARY =
  "inline-flex w-full items-center justify-center gap-2 rounded-lg bg-foreground px-4 py-2.5 " +
  "text-sm font-medium text-background " +
  "transition-opacity hover:opacity-90 disabled:opacity-50 " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30 focus-visible:ring-offset-2 focus-visible:ring-offset-background"

/** Field label. */
export const LABEL = "block text-sm font-medium"

/** Card surface used by the forms and tiles. */
export const CARD = "admin-paper rounded-xl"

/** Inline error banner. */
export const ALERT =
  "rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive"

/** Page title above a form card. Left-aligned — a centred stack reads as a splash. */
export const FORM_TITLE = "text-3xl font-semibold tracking-tight"

/** Descriptive sentence under the title. */
export const FORM_SUBTITLE = "mt-2 text-sm text-muted-foreground"

/** Heading of a listing page, above the table. */
export const PAGE_TITLE = "text-2xl font-semibold tracking-tight"

/**
 * Table shell. Scrolls inside itself so a wide row never makes the page body
 * scroll sideways.
 */
export const TABLE_WRAP = "admin-paper overflow-x-auto rounded-xl"

export const TABLE = "w-full text-left text-sm"

export const THEAD =
  "border-b text-xs uppercase tracking-wide text-muted-foreground"

export const TH = "px-4 py-3 font-medium"

export const TD = "px-4 py-3"

export const ROW = "border-b transition-colors last:border-0 hover:bg-muted/40"

/** Cell holding a date or a count: aligned digits so columns scan vertically. */
export const TD_NUM = "px-4 py-3 text-muted-foreground tabular-nums"

/** Full-width cell for the loading / empty / error states of a table. */
export const TD_STATE = "px-4 py-10 text-center text-muted-foreground"

/**
 * Row-level action. Neutral until hovered, so a row of three buttons reads as
 * one quiet group rather than three competing calls to action.
 */
export const BUTTON_ROW =
  "rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors " +
  "hover:bg-muted hover:text-foreground disabled:opacity-50"

/** Row-level action whose outcome destroys something. */
export const BUTTON_ROW_DANGER =
  "rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors " +
  "hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"

/** Row-level action whose outcome is reversible but restrictive (ban, expire). */
export const BUTTON_ROW_WARN =
  "rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors " +
  "hover:bg-amber-500/10 hover:text-amber-600 disabled:opacity-50"

/**
 * Compact inline primary, for a form that sits inside a page rather than being
 * the page. Same neutral ink as {@link BUTTON_PRIMARY}, without its full width.
 */
export const BUTTON_PRIMARY_INLINE =
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-lg bg-foreground px-4 py-2.5 " +
  "text-sm font-medium text-background " +
  "transition-opacity hover:opacity-90 disabled:opacity-50 " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30"

/**
 * Bordered chip. State is carried by the label too, never by colour alone —
 * colour does not survive a colour-blind reader or a greyscale print.
 */
export const BADGE_BASE =
  "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium"

export const BADGE_TONES = {
  ok: "border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  danger: "border-destructive/25 bg-destructive/10 text-destructive",
  warn: "border-amber-500/25 bg-amber-500/10 text-amber-600 dark:text-amber-400",
  accent:
    "border-violet-500/25 bg-violet-500/10 text-violet-600 dark:text-violet-400",
  muted: "border-border bg-muted text-muted-foreground",
} as const

/** Inline success banner, mirroring {@link ALERT}. */
export const ALERT_OK =
  "rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-600"
