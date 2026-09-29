import { useState, type FormEvent } from "react"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import { withSignUpName } from "../signup-name"
import { oauthErrorCallback } from "../oauth-error"
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
  optional: "facultatif",
  emailPlaceholder: "Adresse e-mail",
  passwordPlaceholder: "Mot de passe",
  passwordHint: `Au moins ${MIN_PASSWORD_LENGTH} caractères.`,
  confirmPlaceholder: "Confirme le mot de passe",
  passwordMismatch: "Les deux mots de passe ne correspondent pas",
  submit: "Créer mon compte",
  submitPending: "Création…",
  haveAccount: "Tu as déjà un compte ?",
  login: "Se connecter",
  emailRequired: "Renseigne ton adresse e-mail",
  passwordTooShort: `Le mot de passe doit faire au moins ${MIN_PASSWORD_LENGTH} caractères`,
  signUpFailed: "La création du compte a échoué",
  alreadyRegistered:
    "Tu as déjà un compte L'Alternative avec cette adresse. Connecte-toi avec ton mot de passe, ou reçois un code par e-mail.",
  // Account linking is off, so signing up with Google
  // on an address already registered is refused rather than folded into the
  // existing account.
  accountNotLinked:
    "Cette adresse a déjà un compte. Connecte-toi avec ton mot de passe.",
  socialCancelled: "Inscription annulée.",
  socialFailed: "La création du compte a échoué. Réessaie.",
}

export function RegisterForm({
  onSuccess,
  loginUrl = "/login",
  legal,
  socialCallbackUrl = "/",
  errorCallbackUrl,
  socialProviders = [],
  labels,
  submitClassName,
  fieldClassName,
  error: externalError,
  linkComponent,
  collectName = true,
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
      // `name` is always sent: Better Auth types it as a required string and
      // its schema rejects the request before any hook runs, so omitting the
      // key when the field is blank fails with `[body.name] Invalid input`.
      const res = await authClient.signUp.email(
        withSignUpName({ name, email: email.trim(), password }),
      )
      if (res?.error) {
        setError(
          res.error.code === "already_registered"
            ? t.alreadyRegistered
            : (res.error.message ?? t.signUpFailed),
        )
        return
      }
      // The address must be confirmed, so sign-up leaves the
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
        callbackURL: socialCallbackUrl,
        // Resolved here rather than at render: the default is the current page,
        // and this runs in the browser, where there is one.
        errorCallbackURL: oauthErrorCallback(
          errorCallbackUrl,
          "/register",
          typeof window !== "undefined" ? window.location.pathname : undefined,
        ),
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : t.socialFailed)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>

      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        {collectName && (
          <AuthField
            label={t.namePlaceholder}
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={isPending}
            autoComplete="name"
            fieldClassName={fieldClassName}
            hint={
              <span className="text-xs text-muted-foreground">
                {t.optional}
              </span>
            }
          />
        )}

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
          to={loginUrl}
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
