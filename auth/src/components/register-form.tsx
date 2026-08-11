import { useState, type FormEvent } from "react"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import { withInviteToken } from "../invite-token"
import type { RegisterFormLabels, RegisterFormProps } from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { SocialButtons } from "./social-buttons"

const MIN_PASSWORD_LENGTH = 8

const DEFAULTS: Required<RegisterFormLabels> = {
  title: "Créer un compte",
  subtitle: "Nous t'enverrons un code pour confirmer ton adresse e-mail.",
  namePlaceholder: "Nom complet",
  emailPlaceholder: "Adresse e-mail",
  passwordPlaceholder: "Mot de passe",
  passwordHint: `Au moins ${MIN_PASSWORD_LENGTH} caractères.`,
  confirmPlaceholder: "Confirme le mot de passe",
  passwordMismatch: "Les deux mots de passe ne correspondent pas",
  submit: "Créer mon compte",
  submitPending: "Création…",
  haveAccount: "Tu as déjà un compte ?",
  login: "Se connecter",
  nameRequired: "Renseigne ton nom",
  emailRequired: "Renseigne ton adresse e-mail",
  passwordTooShort: `Le mot de passe doit faire au moins ${MIN_PASSWORD_LENGTH} caractères`,
  signUpFailed: "La création du compte a échoué",
}

export function RegisterForm({
  onSuccess,
  loginUrl = "/login",
  legal,
  socialCallbackUrl = "/",
  socialProviders = [],
  labels,
  submitClassName,
  fieldClassName,
  error: externalError,
  linkComponent,
  invite,
  authClient,
}: RegisterFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [name, setName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirmPassword, setConfirmPassword] = useState("")
  const [ownError, setOwnError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

  // What the person just did outranks what happened before they arrived.
  const error = ownError ?? externalError
  const setError = setOwnError

  const tooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH
  const mismatch = confirmPassword.length > 0 && confirmPassword !== password

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setError(t.nameRequired)
      return
    }
    if (!email.trim()) {
      setError(t.emailRequired)
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
    setIsPending(true)
    try {
      const res = await authClient.signUp.email({
        name: name.trim(),
        email: email.trim(),
        password,
      })
      if (res?.error) {
        setError(res.error.message ?? t.signUpFailed)
        return
      }
      // createPlatformAuth sets requireEmailVerification, so sign-up leaves the
      // account unverified and without a session: the caller routes to the OTP
      // step rather than into the app.
      onSuccess?.(email.trim())
    } catch (err) {
      setError(err instanceof Error ? err.message : t.signUpFailed)
    } finally {
      setIsPending(false)
    }
  }

  const handleSocial = async (provider: "google" | "github") => {
    setError(undefined)
    try {
      await authClient.signIn.social({
        provider,
        // The invitation rides the callback: an OAuth sign-up leaves the
        // browser, and the auth handler redeems the token on the way back.
        callbackURL: withInviteToken(socialCallbackUrl, invite),
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : t.signUpFailed)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>

      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthField
          label={t.namePlaceholder}
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          disabled={isPending}
          autoComplete="name"
          fieldClassName={fieldClassName}
        />

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
          fieldClassName={fieldClassName}
        />

        <AuthField
          label={t.passwordPlaceholder}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          disabled={isPending}
          autoComplete="new-password"
          description={t.passwordHint}
          // Only once they have typed something: flagging an untouched field
          // red would scold someone for not having started yet.
          invalid={tooShort}
          fieldClassName={fieldClassName}
        />

        <AuthField
          label={t.confirmPlaceholder}
          type="password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          required
          disabled={isPending}
          autoComplete="new-password"
          invalid={mismatch}
          fieldClassName={fieldClassName}
        />

        <AuthSubmit
          spacedAbove
          pending={isPending}
          pendingLabel={t.submitPending}
          className={submitClassName}
        >
          {t.submit}
        </AuthSubmit>

        {legal && (
          <p className="text-center text-xs leading-relaxed text-muted-foreground">
            {legal}
          </p>
        )}
      </form>

      <SocialButtons
        providers={socialProviders}
        onSelect={handleSocial}
        disabled={isPending}
      />

      <p className="text-center text-sm text-muted-foreground">
        {t.haveAccount}{" "}
        <AuthLink
          to={withInviteToken(loginUrl, invite)}
          as={linkComponent}
          className={AUTH_LINK_CLASS}
        >
          {t.login}
        </AuthLink>
      </p>
    </div>
  )
}

// See LoginForm.defaults.
RegisterForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
