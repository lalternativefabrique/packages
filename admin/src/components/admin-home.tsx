import { useEffect, useState } from "react"
import type { AdminHomeLabels, AdminHomeProps, AdminUser } from "../types"
import { AdminKpi } from "./admin-kpi"

const DEFAULTS: Required<AdminHomeLabels> = {
  title: "Tableau de bord",
  subtitle: "",
  accounts: "Comptes",
  accountsHint: "au total",
  admins: "Admins",
  adminsHint: "rôle admin",
  signups: "Inscrits",
  signupsHint: "{days} derniers jours",
  recentTitle: "Inscriptions récentes",
  recentEmpty: "Aucune inscription.",
  loading: "Chargement…",
  loadFailed: "Impossible de charger les statistiques.",
  adminBadge: "admin",
  fallbackAccount: "compte",
}

/**
 * How many accounts the dashboard reads to compute its figures. The tiles count
 * roles and recent sign-ups client-side, which only the listed page can answer:
 * `total` comes from the API, the rest is derived from what was fetched.
 */
const SAMPLE_SIZE = 100

/** Sign-ups within this window count as recent. */
const RECENT_WINDOW_DAYS = 7

/** How many of them the list shows. */
const RECENT_SHOWN = 5

/**
 * Admin dashboard: account KPIs and the latest sign-ups.
 *
 * Fetches with a bare effect — no react-query dependency, so the package stays
 * portable across apps that cache differently.
 *
 * `before` and `after` are where an app puts what only it can answer: usage,
 * queue depth, credits, per-tier margin. Those differ per product, so they stay
 * out of here rather than being modelled badly for everyone.
 */
export function AdminHome({
  api,
  usersLink,
  labels,
  locale = "fr-FR",
  recentWindowDays = RECENT_WINDOW_DAYS,
  before,
  after,
}: AdminHomeProps) {
  const t = { ...DEFAULTS, ...labels }
  const [users, setUsers] = useState<AdminUser[]>([])
  const [total, setTotal] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let alive = true
    api
      .listUsers({ limit: SAMPLE_SIZE, sortBy: "createdAt", sortDirection: "desc" })
      .then((res) => {
        if (!alive) return
        setUsers(res.users)
        setTotal(res.total ?? res.users.length)
      })
      .catch(() => {
        if (alive) setFailed(true)
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [api])

  const admins = users.filter((u) => u.role === "admin").length
  const cutoff = Date.now() - recentWindowDays * 24 * 60 * 60 * 1000
  const recentCount = users.filter((u) => {
    const at = new Date(u.createdAt).getTime()
    return !Number.isNaN(at) && at >= cutoff
  }).length
  const recent = users.slice(0, RECENT_SHOWN)

  return (
    <div className="flex flex-col gap-8">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t.title}</h1>
        {t.subtitle && (
          <p className="mt-1 text-sm text-muted-foreground">{t.subtitle}</p>
        )}
      </div>

      {failed && <p className="text-sm text-destructive">{t.loadFailed}</p>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        {usersLink({
          className:
            "rounded-xl transition-colors hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          children: (
            <AdminKpi
              label={t.accounts}
              value={total ?? "—"}
              hint={t.accountsHint}
              loading={loading}
            />
          ),
        })}
        <AdminKpi
          label={t.admins}
          value={admins}
          hint={t.adminsHint}
          loading={loading}
        />
        <AdminKpi
          label={t.signups}
          value={`+${recentCount}`}
          hint={t.signupsHint.replace("{days}", String(recentWindowDays))}
          loading={loading}
        />
      </div>

      {before}

      <section className="rounded-xl border bg-card p-5">
        <h2 className="font-medium">{t.recentTitle}</h2>
        {loading ? (
          <p className="mt-3 text-sm text-muted-foreground">{t.loading}</p>
        ) : recent.length === 0 ? (
          <p className="mt-3 text-sm text-muted-foreground">{t.recentEmpty}</p>
        ) : (
          <ul className="mt-3 divide-y divide-border">
            {recent.map((u) => (
              <li
                key={u.id}
                className="flex items-center justify-between py-2 text-sm"
              >
                <span className="min-w-0 truncate">
                  {u.email || u.name || t.fallbackAccount}
                  {u.role === "admin" && (
                    <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                      {t.adminBadge}
                    </span>
                  )}
                </span>
                <span className="shrink-0 pl-3 text-muted-foreground">
                  {formatDate(u.createdAt, locale)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      {after}
    </div>
  )
}

function formatDate(value: string | Date, locale: string): string {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return "—"
  return d.toLocaleDateString(locale, { day: "numeric", month: "short" })
}
