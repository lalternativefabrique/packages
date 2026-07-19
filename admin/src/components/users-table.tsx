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

/**
 * The shared admin users table: list + delete + ban/unban + promote/demote.
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
    <div>
      <div className="mb-6">
        <h1 className="text-2xl font-light tracking-tight">Utilisateurs</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {users.length} compte{users.length > 1 ? "s" : ""}
        </p>
      </div>

      {banner ? (
        <div
          className={
            "mb-4 rounded-lg px-3 py-2 text-sm " +
            (banner.tone === "ok"
              ? "bg-green-500/10 text-green-600"
              : "bg-red-500/10 text-red-600")
          }
        >
          {banner.text}
        </div>
      ) : null}

      <div className="overflow-hidden rounded-xl border">
        <table className="w-full text-left text-sm">
          <thead className="border-b text-xs uppercase text-muted-foreground">
            <tr>
              <th className="px-4 py-3 font-medium">Email</th>
              <th className="px-4 py-3 font-medium">Rôle</th>
              <th className="px-4 py-3 font-medium">Statut</th>
              <th className="px-4 py-3 font-medium">Créé le</th>
              <th className="px-4 py-3" />
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
                <td colSpan={5} className="px-4 py-8 text-center text-red-500">
                  {loadError}
                </td>
              </tr>
            ) : users.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-muted-foreground">
                  Aucun utilisateur.
                </td>
              </tr>
            ) : (
              users.map((u) => (
                <tr key={u.id} className="border-b last:border-0">
                  <td className="px-4 py-3">{u.email}</td>
                  <td className="px-4 py-3">
                    {u.role === "admin" ? (
                      <span className="rounded bg-violet-500/10 px-1.5 py-0.5 text-xs text-violet-500">
                        admin
                      </span>
                    ) : (
                      <span className="text-xs text-muted-foreground">user</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    {u.banned ? (
                      <span className="text-xs text-red-500">suspendu</span>
                    ) : u.emailVerified ? (
                      <span className="text-xs text-green-600">actif</span>
                    ) : (
                      <span className="text-xs text-muted-foreground">non vérifié</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {formatDate(u.createdAt)}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => doRoleToggle(u)}
                        className="rounded px-2 py-1 text-xs hover:bg-foreground/5 disabled:opacity-50"
                      >
                        {u.role === "admin" ? "Rétrograder" : "Promouvoir"}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          u.banned ? doBanToggle(u) : setConfirm({ kind: "ban", user: u })
                        }
                        className="rounded px-2 py-1 text-xs hover:bg-amber-500/10 hover:text-amber-600 disabled:opacity-50"
                      >
                        {u.banned ? "Réactiver" : "Suspendre"}
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setConfirm({ kind: "delete", user: u })}
                        aria-label={`Supprimer ${u.email}`}
                        className="rounded px-2 py-1 text-xs text-muted-foreground hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
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
            className="w-full max-w-md rounded-xl bg-background p-6 shadow-2xl"
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
