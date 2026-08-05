import { useCallback, useEffect, useMemo, useState } from "react"
import type {
  AccountsTableProps,
  AdminInvitation,
  AdminUser,
} from "../types"
import {
  ALERT,
  ALERT_OK,
  BADGE_BASE,
  BADGE_TONES,
  BUTTON_PRIMARY_INLINE,
  BUTTON_ROW,
  BUTTON_ROW_DANGER,
  BUTTON_ROW_WARN,
  CARD,
  INPUT,
  LABEL,
  PAGE_TITLE,
  ROW,
  TABLE,
  TABLE_WRAP,
  TD,
  TD_NUM,
  TD_STATE,
  TH,
  THEAD,
} from "../styles"

function formatDate(value: string | Date): string {
  const d = typeof value === "string" ? new Date(value) : value
  return d.toLocaleDateString("fr-FR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  })
}

function Badge({
  tone,
  children,
}: {
  tone: keyof typeof BADGE_TONES
  children: React.ReactNode
}) {
  return (
    <span className={`${BADGE_BASE} ${BADGE_TONES[tone]}`}>{children}</span>
  )
}

/**
 * One line of the table. An account and a pending invitation are the same
 * question — who has access, on what terms — at two different moments, so they
 * are one list rather than two screens an operator has to match up by eye.
 */
type Row =
  | { kind: "account"; user: AdminUser; grant?: string | null }
  | { kind: "invitation"; invitation: AdminInvitation }

type Confirm =
  | { kind: "delete"; user: AdminUser }
  | { kind: "ban"; user: AdminUser }
  | { kind: "revoke"; invitation: AdminInvitation }
  | null

function isExpired(inv: AdminInvitation): boolean {
  if (!inv.expiresAt) return false
  const d =
    typeof inv.expiresAt === "string" ? new Date(inv.expiresAt) : inv.expiresAt
  return d.getTime() < Date.now()
}

/**
 * Folds users and invitations into one list.
 *
 * A redeemed invitation is not a row of its own: the person it invited now has
 * an account, and showing both would list them twice. It survives only as the
 * grant badge carried by their account row — which is the one thing the users
 * list cannot say on its own.
 */
function buildRows(
  users: AdminUser[],
  invitations: AdminInvitation[],
): Row[] {
  const grantByEmail = new Map<string, string | null | undefined>()
  for (const inv of invitations) {
    if (inv.usedAt) grantByEmail.set(inv.email.toLowerCase(), inv.grant)
  }

  const accounts: Row[] = users.map((user) => ({
    kind: "account",
    user,
    grant: grantByEmail.get(user.email.toLowerCase()),
  }))

  const known = new Set(users.map((u) => u.email.toLowerCase()))
  const pending: Row[] = invitations
    .filter((inv) => !inv.usedAt && !known.has(inv.email.toLowerCase()))
    .map((invitation) => ({ kind: "invitation", invitation }))

  return [...pending, ...accounts]
}

/**
 * Accounts: the users table plus the invitations that have not become accounts
 * yet, in one list.
 *
 * Data comes from two injected ports ({@link AdminUserApi} and the optional
 * {@link AdminInvitationApi}), so the component depends on neither better-auth
 * nor any particular backend — the two apps using it invite through services
 * that share no code.
 *
 * Passing no `invitations` prop degrades it to the plain users table, which is
 * what makes it a safe replacement for {@link UsersTable}.
 */
export function AccountsTable({
  api,
  invitations,
  grantOptions,
  onDeleteUser,
  onError,
  onSuccess,
}: AccountsTableProps) {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [invites, setInvites] = useState<AdminInvitation[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [banner, setBanner] = useState<{
    tone: "ok" | "err"
    text: string
  } | null>(null)
  const [confirm, setConfirm] = useState<Confirm>(null)
  const [busy, setBusy] = useState(false)

  const [email, setEmail] = useState("")
  const [grant, setGrant] = useState(grantOptions?.[0]?.value ?? "")
  const [inviting, setInviting] = useState(false)

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
    Promise.all([
      api.listUsers({ limit: 200, sortBy: "createdAt", sortDirection: "desc" }),
      invitations
        ? invitations.listInvitations()
        : Promise.resolve({ invitations: [] as AdminInvitation[] }),
    ])
      .then(([userRes, inviteRes]) => {
        setUsers(userRes.users)
        setInvites(inviteRes.invitations)
        setLoadError(null)
      })
      .catch((e: unknown) =>
        setLoadError(e instanceof Error ? e.message : "Failed to load accounts"),
      )
      .finally(() => setLoading(false))
  }, [api, invitations])

  useEffect(() => {
    load()
  }, [load])

  const rows = useMemo(() => buildRows(users, invites), [users, invites])
  const pendingCount = rows.filter((r) => r.kind === "invitation").length

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

  const doRevoke = (inv: AdminInvitation) =>
    run("Invitation révoquée", () =>
      invitations?.revokeInvitation
        ? invitations.revokeInvitation(inv.email)
        : Promise.reject(new Error("No revoke method configured")),
    )

  const doResend = (inv: AdminInvitation) =>
    run(`Email envoyé à ${inv.email}`, () =>
      invitations?.resendInvitation
        ? invitations.resendInvitation(inv.email)
        : Promise.reject(new Error("No resend method configured")),
    )

  const doRegenerate = (inv: AdminInvitation) =>
    run("Nouveau lien généré", () =>
      invitations?.regenerateInvitation
        ? invitations.regenerateInvitation(inv.email)
        : Promise.reject(new Error("No regenerate method configured")),
    )

  const doCopyLink = async (inv: AdminInvitation) => {
    if (!inv.inviteUrl) return
    try {
      await navigator.clipboard.writeText(inv.inviteUrl)
      notifyOk("Lien copié")
    } catch {
      notifyErr("Impossible de copier le lien")
    }
  }

  const submitInvite = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!invitations || !email.trim()) return
    setInviting(true)
    try {
      await invitations.inviteUser({
        email: email.trim(),
        grant: grant || undefined,
      })
      notifyOk(
        invitations.resendInvitation
          ? `Invitation créée pour ${email.trim()}`
          : `Invitation envoyée à ${email.trim()}`,
      )
      setEmail("")
      load()
    } catch (err: unknown) {
      notifyErr(err instanceof Error ? err.message : "Invitation failed")
    } finally {
      setInviting(false)
    }
  }

  const grantLabel = (value?: string | null) =>
    grantOptions?.find((o) => o.value === value)?.label ?? value

  const colSpan = 5

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className={PAGE_TITLE}>Comptes</h1>
        <p className="text-sm text-muted-foreground tabular-nums">
          {users.length} compte{users.length > 1 ? "s" : ""}
          {pendingCount > 0 ? ` · ${pendingCount} en attente` : ""}
        </p>
      </div>

      {invitations ? (
        <form onSubmit={submitInvite} className={`${CARD} p-5`}>
          <div className="grid gap-4 sm:grid-cols-[1fr_auto_auto] sm:items-end">
            <div className="space-y-1.5">
              <label htmlFor="invite-email" className={LABEL}>
                Adresse e-mail
              </label>
              <input
                id="invite-email"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="marie@example.fr"
                className={INPUT}
              />
            </div>

            {grantOptions && grantOptions.length > 0 ? (
              <div className="space-y-1.5">
                <label htmlFor="invite-grant" className={LABEL}>
                  Accès offert
                </label>
                <select
                  id="invite-grant"
                  value={grant}
                  onChange={(e) => setGrant(e.target.value)}
                  className={INPUT}
                >
                  {grantOptions.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}

            <button
              type="submit"
              disabled={inviting || !email.trim()}
              className={BUTTON_PRIMARY_INLINE}
            >
              {inviting ? "Envoi…" : "Inviter"}
            </button>
          </div>
        </form>
      ) : null}

      {banner ? (
        <div role="status" className={banner.tone === "ok" ? ALERT_OK : ALERT}>
          {banner.text}
        </div>
      ) : null}

      <div className={TABLE_WRAP}>
        <table className={TABLE}>
          <thead className={THEAD}>
            <tr>
              <th className={TH}>Email</th>
              <th className={TH}>Rôle</th>
              <th className={TH}>Statut</th>
              <th className={TH}>Créé le</th>
              <th className={`${TH} text-right`}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={colSpan} className={TD_STATE}>
                  Chargement…
                </td>
              </tr>
            ) : loadError ? (
              <tr>
                <td
                  colSpan={colSpan}
                  className="px-4 py-10 text-center text-destructive"
                >
                  {loadError}
                </td>
              </tr>
            ) : rows.length === 0 ? (
              <tr>
                <td colSpan={colSpan} className={TD_STATE}>
                  Aucun compte.
                </td>
              </tr>
            ) : (
              rows.map((row) =>
                row.kind === "invitation" ? (
                  <tr
                    key={`inv:${row.invitation.email}`}
                    className={`${ROW} bg-muted/20`}
                  >
                    <td className={TD}>
                      <div className="font-medium text-muted-foreground">
                        {row.invitation.email}
                      </div>
                      {row.invitation.reason ? (
                        <div className="text-xs text-muted-foreground/70">
                          {row.invitation.reason}
                        </div>
                      ) : null}
                    </td>
                    <td className={TD}>
                      {row.invitation.grant ? (
                        <Badge tone="muted">
                          {grantLabel(row.invitation.grant)}
                        </Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className={TD}>
                      {isExpired(row.invitation) ? (
                        <Badge tone="muted">expirée</Badge>
                      ) : (
                        <Badge tone="warn">invité</Badge>
                      )}
                    </td>
                    <td className={TD_NUM}>
                      {row.invitation.expiresAt
                        ? `expire le ${formatDate(row.invitation.expiresAt)}`
                        : "—"}
                    </td>
                    <td className={TD}>
                      <div className="flex items-center justify-end gap-1">
                        {row.invitation.inviteUrl ? (
                          <button
                            type="button"
                            onClick={() => doCopyLink(row.invitation)}
                            aria-label={`Copier le lien d'invitation de ${row.invitation.email}`}
                            className={BUTTON_ROW}
                          >
                            Copier le lien
                          </button>
                        ) : null}
                        {invitations?.resendInvitation ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => doResend(row.invitation)}
                            aria-label={`Envoyer l'invitation à ${row.invitation.email}`}
                            className={BUTTON_ROW}
                          >
                            Envoyer l'email
                          </button>
                        ) : null}
                        {invitations?.regenerateInvitation ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => doRegenerate(row.invitation)}
                            aria-label={`Régénérer le lien d'invitation de ${row.invitation.email}`}
                            className={BUTTON_ROW}
                          >
                            Régénérer
                          </button>
                        ) : null}
                        {invitations?.revokeInvitation ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() =>
                              setConfirm({
                                kind: "revoke",
                                invitation: row.invitation,
                              })
                            }
                            aria-label={`Révoquer l'invitation de ${row.invitation.email}`}
                            className={BUTTON_ROW_DANGER}
                          >
                            Révoquer
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr key={row.user.id} className={ROW}>
                    <td className={TD}>
                      <div className="font-medium">{row.user.email}</div>
                      {row.user.name ? (
                        <div className="text-xs text-muted-foreground">
                          {row.user.name}
                        </div>
                      ) : null}
                    </td>
                    <td className={TD}>
                      <div className="flex flex-wrap items-center gap-1.5">
                        {row.user.role === "admin" ? (
                          <Badge tone="accent">admin</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            user
                          </span>
                        )}
                        {row.grant ? (
                          <Badge tone="muted">{grantLabel(row.grant)}</Badge>
                        ) : null}
                      </div>
                    </td>
                    <td className={TD}>
                      {row.user.banned ? (
                        <Badge tone="danger">suspendu</Badge>
                      ) : row.user.emailVerified ? (
                        <Badge tone="ok">actif</Badge>
                      ) : (
                        <Badge tone="muted">non vérifié</Badge>
                      )}
                    </td>
                    <td className={TD_NUM}>{formatDate(row.user.createdAt)}</td>
                    <td className={TD}>
                      <div className="flex items-center justify-end gap-1">
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => doRoleToggle(row.user)}
                          className={BUTTON_ROW}
                        >
                          {row.user.role === "admin"
                            ? "Rétrograder"
                            : "Promouvoir"}
                        </button>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() =>
                            row.user.banned
                              ? doBanToggle(row.user)
                              : setConfirm({ kind: "ban", user: row.user })
                          }
                          className={BUTTON_ROW_WARN}
                        >
                          {row.user.banned ? "Réactiver" : "Suspendre"}
                        </button>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() =>
                            setConfirm({ kind: "delete", user: row.user })
                          }
                          aria-label={`Supprimer ${row.user.email}`}
                          className={BUTTON_ROW_DANGER}
                        >
                          Supprimer
                        </button>
                      </div>
                    </td>
                  </tr>
                ),
              )
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
            ) : confirm.kind === "ban" ? (
              <>
                <h2 className="text-lg font-semibold">Suspendre ce compte ?</h2>
                <p className="mt-3 text-sm text-muted-foreground">
                  {confirm.user.email} ne pourra plus se connecter jusqu'à
                  réactivation. Le compte et ses données sont conservés.
                </p>
              </>
            ) : (
              <>
                <h2 className="text-lg font-semibold">
                  Révoquer cette invitation ?
                </h2>
                <p className="mt-3 text-sm text-muted-foreground">
                  Le lien envoyé à {confirm.invitation.email} cessera de
                  fonctionner. Vous pourrez réinviter cette adresse plus tard.
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
                    : confirm.kind === "ban"
                      ? doBanToggle(confirm.user)
                      : doRevoke(confirm.invitation)
                }
                className={
                  "rounded-lg px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50 " +
                  (confirm.kind === "ban"
                    ? "bg-amber-500 hover:bg-amber-600"
                    : "bg-red-500 hover:bg-red-600")
                }
              >
                {busy
                  ? "…"
                  : confirm.kind === "delete"
                    ? "Supprimer"
                    : confirm.kind === "ban"
                      ? "Suspendre"
                      : "Révoquer"}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
