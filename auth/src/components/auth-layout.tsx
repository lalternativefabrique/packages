import type { AuthLayoutProps } from "../types"

/**
 * The frame every auth screen sits in.
 *
 * Mobile and desktop are two layouts, not one scaled down. On a phone the form
 * IS the page: no card, no border, edge-to-edge padding, top-aligned so the
 * fields stay above the keyboard instead of being pushed under it by vertical
 * centering. From sm up it becomes a bounded card, centered, on a tinted
 * ground — a full-width form on a 1440px display is unreadable.
 *
 * min-h-dvh rather than min-h-screen: on mobile browsers 100vh includes the
 * retracting URL bar, so a screen-height container overflows by its height.
 */
export function AuthLayout({
  logo,
  title,
  subtitle,
  children,
  footer,
}: AuthLayoutProps) {
  return (
    <div className="flex min-h-dvh flex-col bg-background px-5 pb-10 pt-12 sm:items-center sm:justify-center sm:bg-muted/40 sm:px-6 sm:py-12">
      <div className="mx-auto w-full max-w-[420px]">
        <div className="mb-8 text-center sm:mb-7">
          {logo && <div className="mb-5 flex justify-center">{logo}</div>}
          <p className="text-sm font-medium tracking-[0.02em] text-foreground">
            {title}
          </p>
          {subtitle && (
            <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>
          )}
        </div>

        <div className="sm:rounded-2xl sm:border sm:border-border/70 sm:bg-card sm:p-8 sm:shadow-lg sm:shadow-black/5">
          {children}
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
