import type { AuthLayoutProps } from "../types"
import { AuthHeading } from "./auth-heading"

/**
 * The frame every auth screen sits in. It owns the ground, the heading and the
 * app's own marks (logo, illustration, legal footer); the card holds only the
 * fields and their actions.
 *
 * The heading sits ABOVE the card rather than inside it: the card is then
 * exactly the thing you fill in, and the mark reads as the app's rather than
 * as the form's first row.
 *
 * Mobile and desktop are two layouts, not one scaled down. On a phone the form
 * IS the page: no card, no border, edge-to-edge padding, top-aligned so the
 * fields stay above the keyboard instead of being pushed under it by vertical
 * centering. From sm up it becomes a bounded card on a tinted ground — a
 * full-width form on a 1440px display is unreadable.
 *
 * A `panel` is an app-supplied illustration and nothing else: without one the
 * card stays a single column rather than inventing decoration to fill a half
 * it has no content for. When given, it takes the LEFT half from md up and is
 * dropped below that width — a decorative half-screen above a form costs a
 * full swipe before the first field.
 *
 * min-h-dvh rather than min-h-screen: on mobile browsers 100vh includes the
 * retracting URL bar, so a screen-height container overflows by its height.
 */
export function AuthLayout({
  logo,
  panel,
  title,
  subtitle,
  titleClassName,
  children,
  footer,
}: AuthLayoutProps) {
  return (
    <div className="flex min-h-dvh flex-col bg-background px-5 pb-10 pt-12 sm:items-center sm:bg-muted/40 sm:px-6 sm:py-16">
      <div className={`mx-auto w-full ${panel ? "max-w-4xl" : "max-w-[420px]"}`}>
        {(logo || title) && (
          <div className="mb-7 space-y-5">
            {logo && <div className="flex justify-center">{logo}</div>}
            {title && (
              <AuthHeading
                title={title}
                subtitle={subtitle}
                titleClassName={titleClassName}
              />
            )}
          </div>
        )}

        <div
          className={[
            "sm:rounded-lg sm:border sm:border-border/70 sm:bg-card sm:shadow-sm",
            panel ? "sm:overflow-hidden md:grid md:grid-cols-2" : "",
          ]
            .filter(Boolean)
            .join(" ")}
        >
          {panel && (
            // Decorative: it must never carry information the form does not,
            // so it is hidden from assistive tech rather than described.
            <div
              aria-hidden="true"
              className="relative hidden overflow-hidden bg-muted md:block [&>*]:absolute [&>*]:inset-0 [&>*]:h-full [&>*]:w-full [&>img]:object-cover"
            >
              {panel}
            </div>
          )}

          <div className="flex flex-col justify-center sm:p-8">{children}</div>
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
