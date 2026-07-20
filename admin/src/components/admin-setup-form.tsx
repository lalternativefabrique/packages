import { useState, type FormEvent } from "react"
import type { AdminSetupFormProps, AdminSetupLabels } from "../types"
import { ALERT, BUTTON_PRIMARY, CARD, INPUT, LABEL } from "../styles"

const DEFAULT_LABELS: AdminSetupLabels = {
  name: "Nom",
  email: "Email",
  password: "Mot de passe",
  passwordHint: "Min. 8 caractères",
  submit: "Créer le compte admin",
  submitting: "Création…",
  created: "Compte administrateur créé.",
  redirecting: "Redirection…",
  setupFailed: "La configuration a échoué",
}

/**
 * First-admin bootstrap form. The actual creation (a direct SQL insert, run
 * before any admin exists and thus before the normal auth flow can gate it)
 * stays app-side and is passed as `onSubmit`. This component only collects the
 * fields and reports success/error.
 *
 * Renders the card only — no page shell. The app owns centering, background and
 * width, so it can place the form alongside its own chrome (theme/language
 * switchers, footer links) without fighting a nested full-screen wrapper.
 */
export function AdminSetupForm({
  onSubmit,
  onSuccess,
  title = "Configuration initiale",
  subtitle = "Créer le premier compte administrateur",
  labels,
}: AdminSetupFormProps) {
  const t = { ...DEFAULT_LABELS, ...labels }
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
      setError(err instanceof Error ? err.message : t.setupFailed)
    } finally {
      setSubmitting(false)
    }
  }

  if (done) {
    return (
      <div className="rounded-xl border bg-card p-8 text-center shadow-sm">
        <h2 className="text-base font-medium">{t.created}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t.redirecting}</p>
      </div>
    )
  }

  return (
    <div>
      <div className="mb-6 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">{subtitle}</p>
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
          <label htmlFor="setup-name" className={LABEL}>
            {t.name}
          </label>
          <input
            id="setup-name"
            type="text"
            required
            autoComplete="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={INPUT}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="setup-email" className={LABEL}>
            {t.email}
          </label>
          <input
            id="setup-email"
            type="email"
            required
            autoComplete="email"
            placeholder="admin@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className={INPUT}
          />
        </div>

        <div className="space-y-1.5">
          <label htmlFor="setup-password" className={LABEL}>
            {t.password}
          </label>
          <input
            id="setup-password"
            type="password"
            required
            minLength={8}
            autoComplete="new-password"
            placeholder={t.passwordHint}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className={INPUT}
          />
        </div>

        <button
          type="submit"
          disabled={submitting}
          className={BUTTON_PRIMARY}
        >
          {submitting ? t.submitting : t.submit}
        </button>
      </form>
    </div>
  )
}
