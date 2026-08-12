import type { AdminApp, AdminAppTone, AdminLayoutProps } from "../types"

/**
 * Back-office shell: a sticky header carrying the section name, the app's nav
 * links and an optional "back to app" escape hatch, over a centered content
 * column.
 *
 * Purely visual — it takes rendered nodes for the links so the package stays
 * decoupled from the app's router. The role guard lives in the app's route
 * `beforeLoad` (see `hasAdminFeatures`).
 *
 * The app badge and the section name are separated from the nav by hairline
 * rules rather than mere spacing, so "which product", "which section" and
 * "where can I go" don't read as one list.
 *
 * Nav items arrive as opaque router nodes, so the tab shape (padding, radius,
 * hover surface) is applied from here with a child selector rather than by the
 * caller. Apps keep passing plain <Link>s and still get a real hit target; any
 * class they set themselves is appended after these and wins on conflict.
 */
const NAV_ITEM =
  "[&>*]:rounded-md [&>*]:px-2.5 [&>*]:py-1.5 [&>*]:font-medium [&>*]:transition-colors hover:[&>*]:bg-muted"

const APP_DOT: Record<AdminAppTone, string> = {
  violet: "bg-violet-500",
  indigo: "bg-indigo-500",
  emerald: "bg-emerald-500",
  blue: "bg-blue-500",
  amber: "bg-amber-500",
  neutral: "bg-foreground/60",
}

/**
 * Product badge. The name is what a reader recognises, so it carries the weight;
 * the dot is a second, faster cue and never the only one — a tone alone would be
 * unreadable to a colour-blind operator.
 */
function AppBadge({ name, tone = "neutral", logo }: AdminApp) {
  return (
    <span className="flex shrink-0 items-center gap-2">
      {logo ?? (
        <span
          aria-hidden
          className={`size-2 rounded-full ${APP_DOT[tone] ?? APP_DOT.neutral}`}
        />
      )}
      <span className="text-sm font-semibold tracking-tight">{name}</span>
    </span>
  )
}

export function AdminLayout({
  nav,
  backToApp,
  app,
  title = "Administration",
  children,
}: AdminLayoutProps) {
  return (
    <div className="lalt-admin min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/75">
        <div className="mx-auto flex h-16 max-w-6xl items-center gap-4 px-6">
          {app ? (
            <>
              <AppBadge {...app} />
              <span aria-hidden className="h-5 w-px shrink-0 bg-border" />
            </>
          ) : null}
          <span
            className={`shrink-0 text-sm tracking-tight ${
              app ? "text-muted-foreground" : "font-semibold"
            }`}
          >
            {title}
          </span>
          {nav ? (
            <>
              <span aria-hidden className="h-5 w-px shrink-0 bg-border" />
              <nav
                className={`-mx-1 flex items-center gap-0.5 overflow-x-auto px-1 text-sm [scrollbar-width:none] [&::-webkit-scrollbar]:hidden ${NAV_ITEM}`}
              >
                {nav}
              </nav>
            </>
          ) : null}
          {backToApp ? (
            <div className="ml-auto shrink-0 text-sm text-muted-foreground">
              {backToApp}
            </div>
          ) : null}
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-10">{children}</main>
    </div>
  )
}
