/**
 * Shared class strings for the admin surface.
 *
 * The admin is an internal tool, not a product surface: it stays visually
 * neutral and identical across every app rather than wearing each app's brand
 * colour. So the primary action and focus rings are plain black (white on dark)
 * — deliberately NOT the host's `--primary`, which would turn the button green
 * in one app and blue in the next.
 *
 * Surfaces (background, card, border, text) still come from the host tokens, so
 * the admin follows the app's light/dark theme instead of fighting it. Proportions
 * follow the same rhythm as the apps' own auth pages: a roomy field (py-2.5
 * rather than a cramped fixed height) sitting on a ground one step back from the
 * card, so the input reads as a well even before it is focused.
 */

/**
 * Neutral text input. `bg-background` sits a step behind `bg-card`, which is what
 * makes the field legible as an input on a card without a heavy border.
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
export const CARD = "rounded-xl border bg-card shadow-sm"

/** Inline error banner. */
export const ALERT =
  "rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive"

/** Page title above a form card. Left-aligned — a centred stack reads as a splash. */
export const FORM_TITLE = "text-3xl font-semibold tracking-tight"

/** Descriptive sentence under the title. */
export const FORM_SUBTITLE = "mt-2 text-sm text-muted-foreground"
