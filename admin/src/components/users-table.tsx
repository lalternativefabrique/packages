import { useCallback, useEffect, useState } from "react"
import type { AdminUser, UsersTableProps } from "../types"

function formatDate(value: string | Date): string {
  const d = typeof value === "string" ? new Date(value) : value
  return d.toLocaleDateString("fr-FR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  })
}

type Confirm =
  | { kind: "delete"; user: AdminUser }
  | { kind: "ban"; user: AdminUser }
  | null

const PILL_TONES = {
  ok: "border-emerald-500/25 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  danger: "border-destructive/25 bg-destructive/10 text-destructive",
  muted: "border-border bg-muted text-muted-foreground",
} as const

/**
 * Account state as a pill: colour alone would not survive a colour-blind reader
 * or a greyscale print, so the state is also carried by its own label and a
 * bordered chip that reads as a distinct object in the row.
 */
function StatusPill({
  tone,
  children,
}: {
  tone: keyof typeof PILL_TONES
  children: React.ReactNode
}) {
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${PILL_TONES[tone]}`}
    >
      {children}
    </span>
  )
}

/**
 * The shared admin users table: list + delete + ban/unban + promote/demote.
 *
 * @deprecated Prefer {@link AccountsTable}, which shows the same accounts plus
 * the invitations that have not become accounts yet. Called without its
 * `invitations` prop it behaves exactly like this component. Kept working so a
 * minor upgrade never breaks a host that has not migrated.
 *
 * Everything goes through the {@link AdminUserApi} adapter the app passes, so
 * the component depends on neither better-auth nor a router. Delete prefers
 * `onDeleteUser` (the app's server route, which also cleans up domain data);
 * it falls back to `api.removeUser` when only the plugin delete is wired.
 *
 * Feedback uses `onSuccess`/`onError` when provided (e.g. a toast lib), and
 * inline banners otherwise — so apps without `sonner` still get feedback.
 */
export function UsersTable({
  api,
  onDeleteUser,
  onError,
  onSuccess,
}: UsersTableProps) {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [banner, setBanner] = useState<{ tone: "ok" | "err"; text: string } | null>(
    null,
  )
  const [confirm, setConfirm] = useState<Confirm>(null)
  const [busy, setBusy] = useState(false)

  const notifyOk = useCallback(
    (m: string) => (onSuccess ? onSuccess(m) : setBanner({ tone: "ok", text: m })),
    [onSuccess],
  )
  const notifyErr = useCallback(
    (m: string) => (onError ? onError(m) : setBanner({ tone: "err", text: m })),
    [onError],
  )

  const load = useCallback(() => {
    setLoading(true)
    api
      .listUsers({ limit: 200, sortBy: "createdAt", sortDirection: "desc" })
      .then((res) => {
        setUsers(res.users)
        setLoadError(null)
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof Error ? e.message : "Failed to load users"),
      )
      .finally(() => setLoading(false))
  }, [api])

  useEffect(() => {
    load()
  }, [load])

  const run = async (label: string, op: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await op()
      notifyOk(label)
      load()
    } catch (e: unknown) {
      notifyErr(e instanceof Error ? e.message : "Action failed")
    } finally {
      setBusy(false)
      setConfirm(null)
    }
  }

  const doDelete = (u: AdminUser) =>
    run("Compte supprimé", () =>
      onDeleteUser
        ? onDeleteUser(u.id)
        : api.removeUser
          ? api.removeUser(u.id)
          : Promise.reject(new Error("No delete method configured")),
    )

  const doBanToggle = (u: AdminUser) =>
    u.banned
      ? run("Compte réactivé", () => api.unbanUser(u.id))
      : run("Compte suspendu", () => api.banUser(u.id))

  const doRoleToggle = (u: AdminUser) =>
    run(
      u.role === "admin" ? "Rétrogradé en utilisateur" : "Promu administrateur",
      () => api.setRole(u.id, u.role === "admin" ? "user" : "admin"),
    )

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">Utilisateurs</h1>
        <p className="text-sm text-muted-foreground tabular-nums">
          {users.length} compte{users.length > 1 ? "s" : ""}
        </p>
      </div>

      {banner ? (
        <div
          role="status"
          className={
            "rounded-lg border px-3 py-2 text-sm " +
            (banner.tone === "ok"
              ? "border-emerald-500/20 bg-emerald-500/10 text-emerald-600"
              : "border-destructive/20 bg-destructive/10 text-destructive")
          }
        >
          {banner.text}
        </div>
      ) : null}

      {/* Its own scroll container so the page body never scrolls sideways. */}
      <div className="admin-paper overflow-x-auto rounded-xl">
        <table className="w-full text-left text-sm">
          <thead className="border-b bg-muted/40 text-xs uppercase tracking-wide text-muted-foreground">
            <tr>
              <th className="px-4 py-3 font-medium">Email</th>
              <th className="px-4 py-3 font-medium">Rôle</th>
              <th className="px-4 py-3 font-medium">Statut</th>
              <th className="px-4 py-3 font-medium">Créé le</th>
              <th className="px-4 py-3 text-right font-medium">Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-muted-foreground">
                  Chargement…
                </td>
              </tr>
            ) : loadError ? (
              <tr>
                <td colSpan={5} className="px-4 py-10 text-center text-destructive">
                  {loadError}
                </td>
              </tr>
            ) : users.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-10 text-center text-muted-foreground">
                  Aucun utilisateur.
                </td>
              </tr>
            ) : (
              users.map((u) => (
                <tr
                  key={u.id}
                  className="border-b transition-colors last:border-0 hover:bg-muted/40"
                >
                  <td className="px-4 py-3">
                    <div className="font-medium">{u.email}</div>
                    {u.name ? (
                      <div className="text-xs text-muted-foreground">{u.name}</div>
                    ) : null}
                  </td>
                  <td className="px-4 py-3">
                    {u.role === "admin" ? (
                      <span className="inline-flex items-center rounded-full border border-violet-500/25 bg-violet-500/10 px-2 py-0.5 text-xs font-medium text-violet-600 dark:text-violet-400">
                        admin
                      </span>
                    ) : (
                      <span className="text-xs text-muted-foreground">user</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {u.banned ? (
                      <StatusPill tone="danger">suspendu</StatusPill>
                    ) : u.emailVerified ? (
                      <StatusPill tone="ok">actif</StatusPill>
                    ) : (
                      <StatusPill tone="muted">non vérifié</StatusPill>
                    )}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground tabular-nums">
                    {formatDate(u.createdAt)}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => doRoleToggle(u)}
                        className="rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                      >
                        {u.role === "admin" ? "Rétrograder" : "Promouvoir"}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          u.banned ? doBanToggle(u) : setConfirm({ kind: "ban", user: u })
                        }
                        className="rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-amber-500/10 hover:text-amber-600 disabled:opacity-50"
                      >
                        {u.banned ? "Réactiver" : "Suspendre"}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setConfirm({ kind: "delete", user: u })}
                        aria-label={`Supprimer ${u.email}`}
                        className="rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
                      >
                        Supprimer
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {confirm ? (
        <div
          className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/40 px-4"
          onClick={() => !busy && setConfirm(null)}
        >
          <div
            role="dialog"
            aria-modal="true"
            className="w-full max-w-md rounded-xl border bg-card p-6 shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            {confirm.kind === "delete" ? (
              <>
                <h2 className="text-lg font-semibold">Supprimer ce compte ?</h2>
                <p className="mt-3 text-sm text-muted-foreground">
                  {confirm.user.email} sera supprimé, ainsi que ses sessions.
                  Cette action est irréversible.
                </p>
                <p className="mt-2 text-xs text-muted-foreground/70">
                  Ses contenus métier ne sont pas supprimés par cette action.
                </p>
              </>
            ) : (
              <>
                <h2 className="text-lg font-semibold">Suspendre ce compte ?</h2>
                <p className="mt-3 text-sm text-muted-foreground">
                  {confirm.user.email} ne pourra plus se connecter jusqu'à
                  réactivation. Le compte et ses données sont conservés.
                </p>
              </>
            )}
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                disabled={busy}
                onClick={() => setConfirm(null)}
                className="rounded-lg px-3 py-1.5 text-sm text-muted-foreground hover:bg-foreground/5"
              >
                Annuler
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() =>
                  confirm.kind === "delete"
                    ? doDelete(confirm.user)
                    : doBanToggle(confirm.user)
                }
                className={
                  "rounded-lg px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50 " +
                  (confirm.kind === "delete"
                    ? "bg-red-500 hover:bg-red-600"
                    : "bg-amber-500 hover:bg-amber-600")
                }
              >
                {busy
                  ? "…"
                  : confirm.kind === "delete"
                    ? "Supprimer"
                    : "Suspendre"}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
