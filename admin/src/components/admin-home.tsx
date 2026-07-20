import { useEffect, useState } from "react"
import type { AdminHomeProps } from "../types"

/**
 * Admin dashboard. Shows a user-count tile that links to the users page. The
 * link element is app-supplied (router-coupled) via `usersLink`.
 *
 * Fetches the count itself with a bare effect — no react-query dependency, so
 * the package stays portable. Apps that want caching can ignore this and build
 * their own home.
 *
 * The tile is deliberately narrow rather than full-bleed: a single figure
 * stretched across the column reads as an empty card, not as a stat.
 */
export function AdminHome({ api, usersLink, title = "Tableau de bord" }: AdminHomeProps) {
  const [count, setCount] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    api
      .listUsers({ limit: 1 })
      .then((res) => {
        if (alive) setCount(res.total ?? res.users.length)
      })
      .catch(() => {
        if (alive) setCount(null)
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [api])

  return (
    <div className="flex flex-col gap-8">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {usersLink({
          className:
            "group flex flex-col gap-3 rounded-xl border bg-card p-5 shadow-sm transition-colors hover:border-foreground/25 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-foreground/25",
          children: (
            <>
              <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Utilisateurs
              </span>
              {loading ? (
                <span
                  aria-hidden
                  className="h-9 w-12 animate-pulse rounded bg-muted"
                />
              ) : (
                <span className="text-4xl font-semibold tabular-nums leading-none">
                  {count ?? "—"}
                </span>
              )}
              <span className="text-sm text-muted-foreground">
                Voir, modérer et supprimer des comptes
              </span>
            </>
          ),
        })}
      </div>
    </div>
  )
}
