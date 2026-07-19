import type { AdminLayoutProps } from "../types"

/**
 * Back-office shell: header with title, app-supplied nav, and an optional
 * "back to app" link, over a centered content column.
 *
 * Purely visual — it takes rendered nodes for the links so the package stays
 * decoupled from the app's router. The role guard lives in the app's route
 * `beforeLoad` (see `hasAdminFeatures`).
 */
export function AdminLayout({
  nav,
  backToApp,
  title = "Administration",
  children,
}: AdminLayoutProps) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl items-center gap-6 px-6 py-4">
          <span className="text-sm font-medium">{title}</span>
          {nav ? (
            <nav className="flex items-center gap-4 text-sm">{nav}</nav>
          ) : null}
          {backToApp ? <div className="ml-auto text-sm">{backToApp}</div> : null}
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-6 py-8">{children}</main>
    </div>
  )
}
