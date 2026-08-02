import type { AdminLayoutProps } from "../types"

/**
 * Back-office shell: a sticky header carrying the section name, the app's nav
 * links and an optional "back to app" escape hatch, over a centered content
 * column.
 *
 * Purely visual — it takes rendered nodes for the links so the package stays
 * decoupled from the app's router. The role guard lives in the app's route
 * `beforeLoad` (see `hasAdminFeatures`).
 *
 * The wordmark is separated from the nav by a hairline rule rather than mere
 * spacing, so "where am I" and "where can I go" don't read as one list.
 *
 * Nav items arrive as opaque router nodes, so the tab shape (padding, radius,
 * hover surface) is applied from here with a child selector rather than by the
 * caller. Apps keep passing plain <Link>s and still get a real hit target; any
 * class they set themselves is appended after these and wins on conflict.
 */
const NAV_ITEM =
  "[&>*]:rounded-md [&>*]:px-2.5 [&>*]:py-1.5 [&>*]:font-medium [&>*]:transition-colors hover:[&>*]:bg-muted"

export function AdminLayout({
  nav,
  backToApp,
  title = "Administration",
  children,
}: AdminLayoutProps) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/75">
        <div className="mx-auto flex h-16 max-w-6xl items-center gap-4 px-6">
          <span className="shrink-0 text-sm font-semibold tracking-tight">
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
