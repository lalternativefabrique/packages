interface AuthAlertProps {
  children?: string
  tone?: "error" | "success"
}

/**
 * Inline feedback for an auth screen.
 *
 * `role="alert"` on the wrapper is not enough on its own: the node has to be
 * in the tree before the text lands in it for the live region to fire, which
 * is why the element renders empty rather than being mounted with its message.
 *
 * The success tone uses the theme's own tokens rather than a fixed green,
 * which would sit on a dark surface as an unreadable pale block.
 */
export function AuthAlert({ children, tone = "error" }: AuthAlertProps) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      aria-live={tone === "error" ? "assertive" : "polite"}
      className={
        children
          ? [
              "rounded-lg border px-3.5 py-3 text-sm leading-snug",
              "motion-safe:animate-[auth-alert-in_180ms_ease-out]",
              tone === "error"
                ? "border-destructive/25 bg-destructive/10 text-destructive"
                : "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
            ].join(" ")
          : // Empty it must stay in the tree for the live region to fire, but
            // it must not stay in the layout: a `space-y` counts an empty
            // child as a row, which pushed the first field down by a full step
            // on every screen with no message to show.
            "hidden"
      }
    >
      {children}
    </div>
  )
}
