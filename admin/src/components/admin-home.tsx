import { useEffect, useState } from "react"
import type { AdminHomeProps } from "../types"

/**
 * Admin dashboard. Shows a user-count card that links to the users page. The
 * link element is app-supplied (router-coupled) via `usersLink`.
 *
 * Fetches the count itself with a bare effect — no react-query dependency, so
 * the package stays portable. Apps that want caching can ignore this and build
 * their own home.
 */
export function AdminHome({ api, usersLink }: AdminHomeProps) {
  const [count, setCount] = useState<number | null>(null)

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
    return () => {
      alive = false
    }
  }, [api])

  return (
    <div>
      <div className="mb-8">
        <h1 className="text-2xl font-light tracking-tight">Tableau de bord</h1>
        <p className="mt-1 text-sm text-muted-foreground">Administration.</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {usersLink({
          className:
            "group rounded-xl border p-5 transition-colors hover:border-foreground/20",
          children: (
            <>
              <div className="mb-3 flex items-center gap-2 text-muted-foreground group-hover:text-foreground">
                <span className="text-xs font-medium uppercase tracking-wide">
                  Utilisateurs
                </span>
              </div>
              <div className="text-3xl font-light">{count ?? "—"}</div>
              <p className="mt-1 text-sm text-muted-foreground">
                Voir, modérer et supprimer des comptes
              </p>
            </>
          ),
        })}
      </div>
    </div>
  )
}
