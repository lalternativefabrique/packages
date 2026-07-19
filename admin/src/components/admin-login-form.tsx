import { useState, type FormEvent } from "react"
import { hasAdminFeatures } from "../hooks/use-admin"
import type { AdminLoginFormProps } from "../types"

/**
 * Dedicated admin sign-in. Same auth backend as the public login, but bounces
 * non-admins: after sign-in it re-fetches the profile, and if the account is
 * not an admin it signs back out rather than leaving a plain user logged in on
 * the admin surface.
 *
 * The success navigation (to /admin) is the app's job — passed as `onSuccess`
 * so the package stays router-free.
 */
export function AdminLoginForm({
  authClient,
  getProfile,
  onSuccess,
  title = "Connexion admin",
  subtitle = "Administration",
}: AdminLoginFormProps) {
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
        setError(res.error.message ?? "Échec de la connexion")
        return
      }

      const profile = await getProfile()
      if (!hasAdminFeatures(profile)) {
        await authClient.signOut()
        setError("Ce compte n'est pas administrateur.")
        return
      }

      await onSuccess()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Échec de la connexion")
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4 text-foreground">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <p className="text-xs uppercase tracking-widest text-muted-foreground">
            {subtitle}
          </p>
          <h1 className="mt-2 text-2xl font-light tracking-tight">{title}</h1>
        </div>

        <form
          onSubmit={handleSubmit}
          className="space-y-4 rounded-xl border p-6"
        >
          {error ? (
            <div className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-600">
              {error}
            </div>
          ) : null}

          <div>
            <label htmlFor="admin-email" className="mb-1.5 block text-sm font-medium">
              Email
            </label>
            <input
              id="admin-email"
              type="email"
              required
              value={email}
              onChange={(ev) => setEmail(ev.target.value)}
              autoComplete="username"
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div>
            <label
              htmlFor="admin-password"
              className="mb-1.5 block text-sm font-medium"
            >
              Mot de passe
            </label>
            <input
              id="admin-password"
              type="password"
              required
              value={password}
              onChange={(ev) => setPassword(ev.target.value)}
              autoComplete="current-password"
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-lg bg-foreground py-2.5 text-sm font-medium text-background hover:opacity-90 disabled:opacity-50"
          >
            {loading ? "Connexion…" : "Se connecter"}
          </button>
        </form>
      </div>
    </div>
  )
}
