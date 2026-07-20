import { useState, type FormEvent } from "react"
import { hasAdminFeatures } from "../hooks/use-admin"
import type { AdminLoginFormProps, AdminLoginLabels } from "../types"
import { ALERT, BUTTON_PRIMARY, CARD, INPUT, LABEL } from "../styles"

const DEFAULT_LABELS: AdminLoginLabels = {
  email: "Email",
  password: "Mot de passe",
  submit: "Se connecter",
  submitting: "Connexion\u2026",
  signInFailed: "\u00c9chec de la connexion",
  notAnAdmin: "Ce compte n'est pas administrateur.",
}

/**
 * Dedicated admin sign-in. Same auth backend as the public login, but bounces
 * non-admins: after sign-in it re-fetches the profile, and if the account is
 * not an admin it signs back out rather than leaving a plain user logged in on
 * the admin surface.
 *
 * Renders the card only — no page shell. The app owns centering, background and
 * width (and the navigation on success, passed as `onSuccess`), so the form
 * composes with the app's own chrome instead of nesting full-screen wrappers.
 */
export function AdminLoginForm({
  authClient,
  getProfile,
  onSuccess,
  title = "Connexion admin",
  subtitle = "Administration",
  labels,
}: AdminLoginFormProps) {
  const t = { ...DEFAULT_LABELS, ...labels }
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | undefined>()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(undefined)
    setLoading(true)
    try {
      const res = await authClient.signIn.email({ email, password })
      if (res.error) {
        setError(res.error.message ?? t.signInFailed)
        return
      }

      const profile = await getProfile()
      if (!hasAdminFeatures(profile)) {
        await authClient.signOut()
        setError(t.notAnAdmin)
        return
      }

      await onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.signInFailed)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <div className="mb-6 text-center">
        <p className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
          {subtitle}
        </p>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight">{title}</h1>
      </div>

      <form
        onSubmit={handleSubmit}
        className={`space-y-5 p-6 ${CARD}`}
      >
        {error ? (
          <div
            role="alert"
            className={ALERT}
          >
            {error}
          </div>
        ) : null}

        <div className="space-y-1.5">
          <label htmlFor="admin-email" className={LABEL}>
            {t.email}
          </label>
          <input
            id="admin-email"
            type="email"
            required
            value={email}
            onChange={(ev) => setEmail(ev.target.value)}
            autoComplete="username"
            className={INPUT}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="admin-password" className={LABEL}>
            {t.password}
          </label>
          <input
            id="admin-password"
            type="password"
            required
            value={password}
            onChange={(ev) => setPassword(ev.target.value)}
            autoComplete="current-password"
            className={INPUT}
          />
        </div>

        <button
          type="submit"
          disabled={loading}
          className={BUTTON_PRIMARY}
        >
          {loading ? t.submitting : t.submit}
        </button>
      </form>
    </div>
  )
}
