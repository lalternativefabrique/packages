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
 * the admin follows the app's light/dark theme instead of fighting it.
 */

/** Neutral text input: 40px tall, visible border, keyboard-visible focus. */
export const INPUT =
  "h-10 w-full rounded-lg border bg-background px-3 text-sm outline-none transition-colors " +
  "placeholder:text-muted-foreground/60 " +
  "focus-visible:border-foreground/40 focus-visible:ring-2 focus-visible:ring-foreground/15"

/** Neutral primary button: black on light, white on dark. */
export const BUTTON_PRIMARY =
  "h-10 w-full rounded-lg bg-foreground text-sm font-medium text-background " +
  "transition-opacity hover:opacity-90 disabled:opacity-50 " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/30 focus-visible:ring-offset-2 focus-visible:ring-offset-background"

/** Field label. */
export const LABEL = "block text-sm font-medium"

/** Card surface used by the forms and tiles. */
export const CARD = "rounded-xl border bg-card shadow-sm"

/** Inline error banner. */
export const ALERT =
  "rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive"
