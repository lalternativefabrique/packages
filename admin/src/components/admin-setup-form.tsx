import { useState, type FormEvent } from "react"
import type { AdminSetupFormProps, AdminSetupLabels } from "../types"
import {
  ALERT,
  BUTTON_PRIMARY,
  CARD,
  FORM_SUBTITLE,
  FORM_TITLE,
  INPUT,
  LABEL,
} from "../styles"

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
  code: "Code de vérification",
  codeHint: "Le code à 6 chiffres reçu par email",
  codeSent: "Un code a été envoyé à",
  sendCode: "Recevoir le code",
  sendingCode: "Envoi…",
  resendCode: "Renvoyer le code",
  verifyAndCreate: "Vérifier et créer le compte",
  back: "Modifier les informations",
}

/**
 * First-admin bootstrap form. The actual creation (a direct SQL insert, run
 * before any admin exists and thus before the normal auth flow can gate it)
 * stays app-side and is passed as `onSubmit`. This component only collects the
 * fields and reports success/error.
 *
 * Renders the card only — no page shell — at a readable default width
 * (`w-full max-w-sm`, override via `className`). The app owns centering and
 * page background, so it can place the form alongside its own chrome
 * (theme/language switchers, footer links) without nesting full-screen
 * wrappers. The width lives here because it is a property of the form, not of
 * where it sits: leaving it to the app made the card span the whole viewport
 * wherever the wrapper forgot a max-width.
 */
export function AdminSetupForm({
  onSubmit,
  onRequestCode,
  onSuccess,
  title = "Configuration initiale",
  subtitle = "Créer le premier compte administrateur",
  labels,
  className = "",
  icon,
  footer,
}: AdminSetupFormProps) {
  const t = { ...DEFAULT_LABELS, ...labels }
  const [name, setName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [step, setStep] = useState<"details" | "code">("details")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const create = async () => {
    await onSubmit(
      onRequestCode ? { name, email, password, code } : { name, email, password },
    )
    setDone(true)
    await onSuccess?.()
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      // Without a mailer the details step IS the whole form, and this stays the
      // single-step component it has always been.
      if (onRequestCode && step === "details") {
        await onRequestCode(email)
        setStep("code")
        return
      }
      await create()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.setupFailed)
    } finally {
      setSubmitting(false)
    }
  }

  const resend = async () => {
    if (!onRequestCode) return
    setError(null)
    setSubmitting(true)
    try {
      await onRequestCode(email)
    } catch (err) {
      setError(err instanceof Error ? err.message : t.setupFailed)
    } finally {
      setSubmitting(false)
    }
  }

  if (done) {
    return (
      <div className={`lalt-admin w-full max-w-md ${className}`}>
        <div className={`${CARD} p-8 text-center`}>
          <h2 className="text-base font-medium">{t.created}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t.redirecting}</p>
        </div>
      </div>
    )
  }

  return (
    <div className={`lalt-admin w-full max-w-md ${className}`}>
      <h1 className={FORM_TITLE}>{title}</h1>
      {subtitle ? <p className={FORM_SUBTITLE}>{subtitle}</p> : null}

      <form onSubmit={handleSubmit} className={`mt-8 space-y-5 p-6 ${CARD}`}>
        {error ? (
          <div role="alert" className={ALERT}>
            {error}
          </div>
        ) : null}

        {step === "code" ? (
          <>
            <p className="text-sm text-muted-foreground">
              {t.codeSent} <span className="font-medium">{email}</span>.
            </p>
            <div className="space-y-1.5">
              <label htmlFor="setup-code" className={LABEL}>
                {t.code}
              </label>
              <input
                id="setup-code"
                type="text"
                required
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder={t.codeHint}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                className={INPUT}
              />
            </div>

            <button type="submit" disabled={submitting} className={BUTTON_PRIMARY}>
              {icon}
              {submitting ? t.submitting : t.verifyAndCreate}
            </button>

            <div className="flex justify-between text-sm">
              <button
                type="button"
                onClick={() => {
                  setError(null)
                  setStep("details")
                }}
                className="text-muted-foreground underline-offset-4 hover:underline"
              >
                {t.back}
              </button>
              <button
                type="button"
                onClick={resend}
                disabled={submitting}
                className="text-muted-foreground underline-offset-4 hover:underline disabled:opacity-50"
              >
                {submitting ? t.sendingCode : t.resendCode}
              </button>
            </div>
          </>
        ) : (
          <>
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

        <button type="submit" disabled={submitting} className={BUTTON_PRIMARY}>
          {icon}
          {submitting
            ? onRequestCode
              ? t.sendingCode
              : t.submitting
            : onRequestCode
              ? t.sendCode
              : t.submit}
        </button>
          </>
        )}
      </form>

      {footer ? (
        <div className="mt-5 text-center text-sm text-muted-foreground">
          {footer}
        </div>
      ) : null}
    </div>
  )
}
