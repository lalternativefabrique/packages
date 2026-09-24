import { useState, type FormEvent } from "react"
import type { EmailCodeSignInFormLabels, EmailCodeSignInFormProps } from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"

const DEFAULTS: Required<EmailCodeSignInFormLabels> = {
  title: "Code de connexion",
  subtitle: "Recevoir un code par e-mail",
  emailPlaceholder: "Adresse e-mail",
  codePlaceholder: "Code reçu par e-mail",
  send: "Recevoir un code",
  submit: "Se connecter",
  pending: "Un instant…",
  sendFailed: "Ce code n'a pas pu être envoyé. Vérifie l'adresse.",
  invalidCode: "Code incorrect ou expiré.",
  unavailable: "Le service ne répond pas pour le moment. Réessaie dans un instant.",
  changeEmail: "Changer d'adresse",
  usePassword: "Se connecter avec un mot de passe",
}

type Step = "email" | "code"

// An unknown address is signed up by the same code, so this screen is also
// the passwordless way in for someone who has no account yet.
export function EmailCodeSignInForm({
  onSuccess,
  passwordSignInUrl = "/login",
  coreTokenUrl = "/api/auth/core-token",
  labels,
  submitClassName,
  fieldClassName,
  error: externalError,
  linkComponent,
  authClient,
}: EmailCodeSignInFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [step, setStep] = useState<Step>("email")
  const [email, setEmail] = useState("")
  const [code, setCode] = useState("")
  const [ownError, setOwnError] = useState<string | undefined>()
  const [pending, setPending] = useState(false)
  const error = ownError ?? externalError

  const refusal = (status: number | undefined, fallback: string) =>
    status === 503 ? t.unavailable : fallback

  const sendCode = async () => {
    const { error: failed } = await authClient.emailOtp.sendVerificationOtp({
      email: email.trim(),
      type: "sign-in",
    })
    if (failed) {
      setOwnError(refusal(failed.status, t.sendFailed))
      return
    }
    setStep("code")
  }

  const verifyCode = async () => {
    const { error: failed } = await authClient.signIn.emailOtp({
      email: email.trim(),
      otp: code.trim(),
    })
    if (failed) {
      setOwnError(refusal(failed.status, t.invalidCode))
      return
    }
    if (coreTokenUrl) await fetch(coreTokenUrl, { credentials: "include" }).catch(() => null)
    onSuccess?.()
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setOwnError(undefined)
    setPending(true)
    try {
      await (step === "email" ? sendCode() : verifyCode())
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>
      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthField
          label={t.emailPlaceholder}
          type="email"
          inputMode="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
          disabled={pending || step === "code"}
          autoComplete="email"
          autoCapitalize="none"
          spellCheck={false}
          invalid={!!error}
          fieldClassName={fieldClassName}
        />
        {step === "code" && (
          <AuthField
            label={t.codePlaceholder}
            type="text"
            inputMode="numeric"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
            autoFocus
            disabled={pending}
            autoComplete="one-time-code"
            invalid={!!error}
            fieldClassName={fieldClassName}
          />
        )}
        <AuthSubmit
          spacedAbove
          pending={pending}
          disabled={step === "email" ? !email.trim() : !code.trim()}
          pendingLabel={t.pending}
          className={submitClassName}
        >
          {step === "email" ? t.send : t.submit}
        </AuthSubmit>
      </form>
      <p className="text-center text-sm text-muted-foreground">
        {step === "code" ? (
          <button
            type="button"
            onClick={() => {
              setStep("email")
              setCode("")
              setOwnError(undefined)
            }}
            className={AUTH_LINK_CLASS}
          >
            {t.changeEmail}
          </button>
        ) : (
          <AuthLink to={passwordSignInUrl} as={linkComponent} className={AUTH_LINK_CLASS}>
            {t.usePassword}
          </AuthLink>
        )}
      </p>
    </div>
  )
}

EmailCodeSignInForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
