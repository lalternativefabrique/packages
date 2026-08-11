import type { AuthLayoutProps } from "../types"

/**
 * The frame every auth screen sits in. It owns the ground, the card and the
 * app's own marks (logo, illustration, legal footer); the form it wraps owns
 * its title and its fields.
 *
 * Mobile and desktop are two layouts, not one scaled down. On a phone the form
 * IS the page: no card, no border, edge-to-edge padding, top-aligned so the
 * fields stay above the keyboard instead of being pushed under it by vertical
 * centering. From sm up it becomes a bounded card on a tinted ground — a
 * full-width form on a 1440px display is unreadable.
 *
 * With a `panel` the card splits in two from md up, the form on the left and
 * the illustration on the right. The panel is dropped below that width rather
 * than stacked: a decorative half-screen above a form costs a full swipe
 * before the first field. Without a `panel` the card stays the single 420px
 * column it has always been.
 *
 * min-h-dvh rather than min-h-screen: on mobile browsers 100vh includes the
 * retracting URL bar, so a screen-height container overflows by its height.
 */
export function AuthLayout({
  logo,
  panel,
  children,
  footer,
}: AuthLayoutProps) {
  return (
    <div className="flex min-h-dvh flex-col bg-background px-5 pb-10 pt-12 sm:items-center sm:bg-muted/40 sm:px-6 sm:py-16">
      <div
        className={`mx-auto w-full ${panel ? "max-w-4xl" : "max-w-[420px]"}`}
      >
        <div
          className={[
            "sm:overflow-hidden sm:rounded-2xl sm:border sm:border-border/70 sm:bg-card sm:shadow-lg sm:shadow-black/5",
            panel ? "md:grid md:grid-cols-2" : "",
          ]
            .filter(Boolean)
            .join(" ")}
        >
          <div className="sm:p-8">
            {logo && <div className="mb-7 flex justify-center">{logo}</div>}
            {children}
          </div>

          {panel && (
            // Decorative: it must never carry information the form does not,
            // so it is hidden from assistive tech rather than described.
            <div
              aria-hidden="true"
              className="hidden bg-muted md:block [&>*]:h-full [&>*]:w-full [&>img]:object-cover"
            >
              {panel}
            </div>
          )}
        </div>

        {footer && (
          <div className="mt-6 text-center text-xs leading-relaxed text-muted-foreground">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
