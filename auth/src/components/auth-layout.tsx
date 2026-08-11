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
    <div
      className={[
        "relative flex min-h-dvh flex-col px-5 pb-12 pt-14 sm:items-center sm:px-6 sm:py-20",
        // With a panel the colour IS the mobile ground and the card sits on
        // it; the split card only exists once there is width for two columns.
        panel
          ? "bg-transparent md:bg-muted/30"
          : "bg-background sm:bg-muted/30",
      ].join(" ")}
    >
      {panel && (
        <div
          aria-hidden="true"
          className="absolute inset-0 -z-10 md:hidden [&>*]:h-full [&>*]:w-full [&>img]:object-cover"
        >
          {panel}
        </div>
      )}

      <div className={`mx-auto w-full ${panel ? "max-w-4xl" : "max-w-[440px]"}`}>
        {(logo || title) && (
          // The mark sits tight above the title so the two read as one block
          // rather than as a stray label; the gap down to the card is the
          // largest on the screen — that step is what separates "who this is"
          // from "what you do here".
          <div
            className={[
              "mb-9 space-y-3",
              // Over the colour field the heading is on the panel, not on the
              // page, so it takes the panel's ink until the split puts it back
              // on a light ground.
              panel
                ? "text-white [&_p]:text-white/75 md:text-foreground md:[&_p]:text-muted-foreground"
                : "",
            ]
              .filter(Boolean)
              .join(" ")}
          >
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
            "sm:rounded-xl sm:border sm:border-foreground/[0.08] sm:bg-card",
            "sm:shadow-[0_1px_2px_rgba(0,0,0,0.04),0_8px_24px_-12px_rgba(0,0,0,0.10)]",
            // Over a colour field the card cannot stay the bare mobile form it
            // is on a white page: it needs its own ground to sit on, from the
            // narrowest width up.
            panel
              ? "-mx-1 rounded-xl border border-black/5 bg-card p-1 shadow-xl sm:mx-0 sm:overflow-hidden sm:p-0 md:grid md:grid-cols-2"
              : "",
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

          <div className={panel ? "p-6 sm:p-7" : "sm:p-7"}>{children}</div>
        </div>

        {footer && (
          <div className="mt-8 text-center text-xs leading-relaxed text-muted-foreground">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
