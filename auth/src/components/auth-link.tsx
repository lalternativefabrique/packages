import type { ReactNode } from "react"
import type { LinkComponent } from "../types"

interface AuthLinkProps {
  to: string
  as?: LinkComponent
  className?: string
  children: ReactNode
}

/**
 * A link between auth screens.
 *
 * Falls back to an anchor, which is right for an app without a router and wrong
 * for every app with one: the full page load it triggers restarts the app and
 * loses whatever the URL was carrying.
 */
export function AuthLink({ to, as: Link, className, children }: AuthLinkProps) {
  if (Link) {
    return (
      <Link to={to} className={className}>
        {children}
      </Link>
    )
  }
  return (
    <a href={to} className={className}>
      {children}
    </a>
  )
}

export const AUTH_LINK_CLASS =
  "rounded font-medium text-foreground underline underline-offset-4 decoration-foreground/25 transition-colors hover:decoration-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"

export const AUTH_HINT_LINK_CLASS =
  "rounded text-xs text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
