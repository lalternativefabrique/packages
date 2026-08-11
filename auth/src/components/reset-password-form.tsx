import { useState, type FormEvent } from "react"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import type { ResetPasswordFormLabels, ResetPasswordFormProps } from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthOtpField, OTP_LENGTH } from "./auth-otp-field"
import { AuthSubmit } from "./auth-submit"

const MIN_PASSWORD_LENGTH = 8

const DEFAULTS: Required<ResetPasswordFormLabels> = {
  title: "Nouveau mot de passe",
  subtitle: "Entre le code à 6 chiffres reçu par e-mail et ton nouveau mot de passe.",
  codePlaceholder: "Code de vérification",
  passwordPlaceholder: "Nouveau mot de passe",
  passwordHint: `Au moins ${MIN_PASSWORD_LENGTH} caractères.`,
  confirmPlaceholder: "Confirme le mot de passe",
  submit: "Réinitialiser",
  submitPending: "Réinitialisation…",
  resend: "Renvoyer le code",
  resendPending: "Envoi…",
  resent: "Un nouveau code vient de t'être envoyé.",
  rememberPassword: "Tu t'en souviens finalement ?",
  login: "Se connecter",
  codeRequired: "Entre le code à 6 chiffres",
  passwordTooShort: `Le mot de passe doit faire au moins ${MIN_PASSWORD_LENGTH} caractères`,
  passwordMismatch: "Les deux mots de passe ne correspondent pas",
  resetFailed: "La réinitialisation a échoué",
  resendFailed: "L'envoi du code a échoué",
}

export function ResetPasswordForm({
  email,
  onSuccess,
  loginUrl = "/login",
  labels,
  submitClassName,
  fieldClassName,
  linkComponent,
  authClient,
}: ResetPasswordFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [otp, setOtp] = useState("")
  const [password, setPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [isResetting, setIsResetting] = useState(false)
  const [isResending, setIsResending] = useState(false)
  const [resendMessage, setResendMessage] = useState<string | undefined>()

  const tooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH
  const mismatch = confirmPassword.length > 0 && confirmPassword !== password

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (otp.length < OTP_LENGTH) {
      setError(t.codeRequired)
      return
    }
    if (password.length < MIN_PASSWORD_LENGTH) {
      setError(t.passwordTooShort)
      return
    }
    if (password !== confirmPassword) {
      setError(t.passwordMismatch)
      return
    }
    setError(undefined)
    setResendMessage(undefined)
    setIsResetting(true)
    try {
      const res = await fetch("/api/auth/email-otp/reset-password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, otp, password }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => null)
        setError(body?.message ?? t.resetFailed)
        return
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.resetFailed)
    } finally {
      setIsResetting(false)
    }
  }

  const handleResend = async () => {
    setError(undefined)
    setResendMessage(undefined)
    setIsResending(true)
    try {
      await authClient.emailOtp.sendVerificationOtp({
        email,
        type: "forget-password",
      })
      setResendMessage(t.resent)
    } catch (err) {
      setError(err instanceof Error ? err.message : t.resendFailed)
    } finally {
      setIsResending(false)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>
      <AuthAlert tone="success">{resendMessage}</AuthAlert>

      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthOtpField
          id="reset-password-otp"
          label={t.codePlaceholder}
          value={otp}
          onChange={setOtp}
          disabled={isResetting}
          fieldClassName={fieldClassName}
        />

        <AuthField
          label={t.passwordPlaceholder}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          disabled={isResetting}
          autoComplete="new-password"
          description={t.passwordHint}
          invalid={tooShort}
        />

        <AuthField
          label={t.confirmPlaceholder}
          type="password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          required
          disabled={isResetting}
          autoComplete="new-password"
          invalid={mismatch}
        />

        <AuthSubmit
          spacedAbove
          pending={isResetting}
          disabled={otp.length < OTP_LENGTH || !password || !confirmPassword}
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
          {t.rememberPassword}{" "}
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
ResetPasswordForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
