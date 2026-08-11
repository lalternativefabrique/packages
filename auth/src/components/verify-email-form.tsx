import { useState, type FormEvent } from "react"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import type { VerifyEmailFormLabels, VerifyEmailFormProps } from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthOtpField, OTP_LENGTH } from "./auth-otp-field"
import { AuthSubmit } from "./auth-submit"

const DEFAULTS: Required<VerifyEmailFormLabels> = {
  title: "Vérifie ton adresse",
  subtitle: "Entre le code à 6 chiffres envoyé à",
  subtitleNoEmail: "Entre le code à 6 chiffres reçu par e-mail.",
  codePlaceholder: "Code de vérification",
  submit: "Vérifier",
  submitPending: "Vérification…",
  resend: "Renvoyer le code",
  resendPending: "Envoi…",
  resent: "Un nouveau code vient de t'être envoyé.",
  alreadyVerified: "Adresse déjà vérifiée ?",
  login: "Se connecter",
  codeRequired: "Entre le code à 6 chiffres",
  invalidCode: "Code invalide. Réessaie.",
  resendFailed: "L'envoi du code a échoué",
  missingEmail: "Adresse e-mail introuvable. Recommence l'inscription.",
}

export function VerifyEmailForm({
  email,
  onSuccess,
  loginUrl = "/login",
  labels,
  submitClassName,
  fieldClassName,
  linkComponent,
  authClient,
}: VerifyEmailFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [otp, setOtp] = useState("")
  const [isVerifying, setIsVerifying] = useState(false)
  const [isResending, setIsResending] = useState(false)
  const [resendMessage, setResendMessage] = useState<string | undefined>()
  const [error, setError] = useState<string | undefined>()

  const handleVerify = async (e: FormEvent) => {
    e.preventDefault()
    if (otp.length < OTP_LENGTH) {
      setError(t.codeRequired)
      return
    }
    setError(undefined)
    setResendMessage(undefined)
    setIsVerifying(true)
    try {
      const res = await authClient.emailOtp.verifyEmail({ email, otp })
      if (res?.error) {
        setError(res.error.message ?? t.invalidCode)
        return
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.invalidCode)
    } finally {
      setIsVerifying(false)
    }
  }

  const handleResend = async () => {
    if (!email) {
      setError(t.missingEmail)
      return
    }
    setError(undefined)
    setResendMessage(undefined)
    setIsResending(true)
    try {
      await authClient.emailOtp.sendVerificationOtp({
        email,
        type: "email-verification",
      })
      setResendMessage(t.resent)
    } catch (err) {
      setError(err instanceof Error ? err.message : t.resendFailed)
    } finally {
      setIsResending(false)
    }
  }

  return (
    <div className="space-y-6">
      <AuthAlert>{error}</AuthAlert>
      <AuthAlert tone="success">{resendMessage}</AuthAlert>

      <form onSubmit={handleVerify} className="space-y-5" noValidate>
        <AuthOtpField
          id="verify-email-otp"
          label={t.codePlaceholder}
          value={otp}
          onChange={setOtp}
          disabled={isVerifying}
          fieldClassName={fieldClassName}
        />

        <AuthSubmit
          pending={isVerifying}
          disabled={otp.length < OTP_LENGTH}
          pendingLabel={t.submitPending}
          className={submitClassName}
        >
          {t.submit}
        </AuthSubmit>
      </form>

      <div className="space-y-4 text-center text-sm text-muted-foreground">
        <button
          type="button"
          onClick={handleResend}
          disabled={isResending}
          className="rounded font-medium text-foreground underline underline-offset-4 decoration-foreground/25 transition-colors hover:decoration-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-55"
        >
          {isResending ? t.resendPending : t.resend}
        </button>

        <p>
          {t.alreadyVerified}{" "}
          <AuthLink
            to={loginUrl}
            as={linkComponent}
            className={AUTH_LINK_CLASS}
          >
            {t.login}
          </AuthLink>
        </p>
      </div>
    </div>
  )
}

// See LoginForm.defaults.
VerifyEmailForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
