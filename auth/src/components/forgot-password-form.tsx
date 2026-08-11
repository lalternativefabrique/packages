import { useState, type FormEvent } from "react"
import type {
  ForgotPasswordFormLabels,
  ForgotPasswordFormProps,
} from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthHeading } from "./auth-heading"
import { AuthSubmit } from "./auth-submit"

const DEFAULTS: Required<ForgotPasswordFormLabels> = {
  title: "Mot de passe oublié ?",
  subtitle:
    "Entre ton adresse e-mail et nous t'enverrons un code pour le réinitialiser.",
  emailPlaceholder: "Adresse e-mail",
  submit: "Envoyer le code",
  submitPending: "Envoi…",
  rememberPassword: "Tu t'en souviens finalement ?",
  login: "Se connecter",
  emailRequired: "Renseigne ton adresse e-mail",
  sendFailed: "L'envoi du code a échoué",
}

export function ForgotPasswordForm({
  onSuccess,
  loginUrl = "/login",
  labels,
  titleClassName,
  authClient,
}: ForgotPasswordFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [email, setEmail] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim()) {
      setError(t.emailRequired)
      return
    }
    setError(undefined)
    setIsPending(true)
    try {
      const res = await authClient.emailOtp.sendVerificationOtp({
        email: email.trim(),
        type: "forget-password",
      })
      if (res?.error) {
        setError(res.error.message ?? t.sendFailed)
        return
      }
      onSuccess?.(email.trim())
    } catch (err) {
      setError(err instanceof Error ? err.message : t.sendFailed)
    } finally {
      setIsPending(false)
    }
  }

  return (
    <div className="space-y-7">
      <AuthHeading
        title={t.title}
        subtitle={t.subtitle}
        titleClassName={titleClassName}
      />

      <AuthAlert>{error}</AuthAlert>

      <form onSubmit={handleSubmit} className="space-y-5" noValidate>
        <AuthField
          label={t.emailPlaceholder}
          type="email"
          inputMode="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          disabled={isPending}
          autoComplete="email"
          autoCapitalize="none"
          spellCheck={false}
          invalid={!!error}
        />

        <AuthSubmit
          pending={isPending}
          disabled={!email.trim()}
          pendingLabel={t.submitPending}
        >
          {t.submit}
        </AuthSubmit>
      </form>

      <p className="text-center text-sm text-muted-foreground">
        {t.rememberPassword}{" "}
        <a
          href={loginUrl}
          className="rounded font-medium text-foreground underline underline-offset-4 decoration-foreground/25 transition-colors hover:decoration-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t.login}
        </a>
      </p>
    </div>
  )
}
