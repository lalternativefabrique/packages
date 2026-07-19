import { useState, type FormEvent } from "react"
import type { AdminSetupFormProps } from "../types"

/**
 * First-admin bootstrap form. The actual creation (a direct SQL insert, run
 * before any admin exists and thus before the normal auth flow can gate it)
 * stays app-side and is passed as `onSubmit`. This component only collects the
 * fields and reports success/error.
 */
export function AdminSetupForm({
  onSubmit,
  onSuccess,
  title = "Configuration initiale",
  subtitle = "Créer le premier compte administrateur",
}: AdminSetupFormProps) {
  const [name, setName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await onSubmit({ name, email, password })
      setDone(true)
      await onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : "La configuration a échoué")
    } finally {
      setSubmitting(false)
    }
  }

  if (done) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background px-4 text-foreground">
        <div className="w-full max-w-md text-center">
          <h2 className="text-lg font-medium">Compte administrateur créé.</h2>
          <p className="mt-1 text-sm text-muted-foreground">Redirection…</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4 text-foreground">
      <div className="w-full max-w-md">
        <div className="mb-10 text-center">
          <h1 className="text-[32px] font-light tracking-tight">{title}</h1>
          <p className="mt-2 text-sm text-muted-foreground">{subtitle}</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4 rounded-xl border p-8">
          {error ? (
            <div className="rounded-lg bg-red-500/10 p-3 text-sm text-red-600">
              {error}
            </div>
          ) : null}

          <div>
            <label htmlFor="setup-name" className="mb-1.5 block text-sm font-medium">
              Nom
            </label>
            <input
              id="setup-name"
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div>
            <label htmlFor="setup-email" className="mb-1.5 block text-sm font-medium">
              Email
            </label>
            <input
              id="setup-email"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div>
            <label
              htmlFor="setup-password"
              className="mb-1.5 block text-sm font-medium"
            >
              Mot de passe
            </label>
            <input
              id="setup-password"
              type="password"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Min. 8 caractères"
              className="w-full rounded-lg border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <button
            type="submit"
            disabled={submitting}
            className="w-full rounded-lg bg-foreground py-2.5 text-sm font-medium text-background hover:opacity-90 disabled:opacity-50"
          >
            {submitting ? "Création…" : "Créer le compte admin"}
          </button>
        </form>

        <p className="mt-4 text-center text-xs text-muted-foreground">
          Cette page n'est disponible que tant qu'aucun administrateur n'existe.
        </p>
      </div>
    </div>
  )
}
