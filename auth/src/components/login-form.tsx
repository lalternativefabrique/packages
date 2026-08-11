import { useState, type FormEvent } from "react"
import type { LoginFormLabels, LoginFormProps } from "../types"
import { isEmailNotVerified } from "../email-not-verified"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { SocialButtons } from "./social-buttons"
import { AUTH_HINT_LINK_CLASS, AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import { withInviteToken } from "../invite-token"

const DEFAULTS: Required<LoginFormLabels> = {
  title: "Connexion",
  subtitle: "Content de te revoir. Entre tes identifiants pour continuer.",
  emailPlaceholder: "Adresse e-mail",
  passwordPlaceholder: "Mot de passe",
  forgotPassword: "Mot de passe oublié ?",
  submit: "Se connecter",
  submitPending: "Connexion…",
  noAccount: "Pas encore de compte ?",
  register: "Créer un compte",
  emailRequired: "Renseigne ton adresse e-mail",
  passwordRequired: "Renseigne ton mot de passe",
  invalidCredentials: "Adresse e-mail ou mot de passe incorrect",
  emailNotVerified:
    "Ton adresse e-mail n'est pas encore confirmée. Vérifie ta boîte de réception.",
  // accountLinking is disabled in createPlatformAuth, so a social sign-in on an
  // address already registered with a password is refused with this code.
  // Without a message the button reads as broken rather than as a rejection.
  accountNotLinked:
    "Cette adresse est déjà associée à un mot de passe. Connecte-toi avec ton mot de passe.",
  socialCancelled: "Connexion annulée.",
  socialFailed: "La connexion a échoué. Réessaie.",
}

export function LoginForm({
  onSuccess,
  onEmailNotVerified,
  registerUrl = "/register",
  forgotPasswordUrl = "/forgot-password",
  socialCallbackUrl = "/",
  socialProviders = [],
  coreTokenUrl = "/api/auth/core-token",
  labels,
  submitClassName,
  fieldClassName,
  error: externalError,
  linkComponent,
  invite,
  authClient,
}: LoginFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [ownError, setOwnError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

  // What the person just did outranks what happened before they arrived.
  const error = ownError ?? externalError
  const setError = setOwnError

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim()) {
      setError(t.emailRequired)
      return
    }
    if (!password) {
      setError(t.passwordRequired)
      return
    }
    setError(undefined)
    setIsPending(true)
    try {
      const res = await authClient.signIn.email({
        email: email.trim(),
        password,
      })
      if (res?.error) {
        if (isEmailNotVerified(res.error) && onEmailNotVerified) {
          onEmailNotVerified(email.trim())
          return
        }
        setError(
          isEmailNotVerified(res.error)
            ? t.emailNotVerified
            : (res.error.message ?? t.invalidCredentials),
        )
        return
      }
      // The better-auth session cookie alone does not authenticate a Go core:
      // it verifies the EdDSA JWT minted here against the issuer's JWKS.
      // Skipped when the app sets the token itself, or has no core at all.
      if (coreTokenUrl) {
        await fetch(coreTokenUrl, { credentials: "include" })
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.invalidCredentials)
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
      const code = err instanceof Error ? err.message : ""
      setError(
        code === "account_not_linked"
          ? t.accountNotLinked
          : code === "access_denied"
            ? t.socialCancelled
            : t.socialFailed,
      )
    }
  }

  return (
    <div className="space-y-6">
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
          fieldClassName={fieldClassName}
        />

        <AuthField
          label={t.passwordPlaceholder}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          disabled={isPending}
          autoComplete="current-password"
          invalid={!!error}
          fieldClassName={fieldClassName}
          hint={
            <AuthLink
              to={forgotPasswordUrl}
              as={linkComponent}
              className={AUTH_HINT_LINK_CLASS}
            >
              {t.forgotPassword}
            </AuthLink>
          }
        />

        <AuthSubmit
          pending={isPending}
          disabled={!email.trim() || !password}
          pendingLabel={t.submitPending}
          className={submitClassName}
        >
          {t.submit}
        </AuthSubmit>
      </form>

      <SocialButtons
        providers={socialProviders}
        onSelect={handleSocial}
        disabled={isPending}
      />

      <p className="text-center text-sm text-muted-foreground">
        {t.noAccount}{" "}
        <AuthLink
          to={withInviteToken(registerUrl, invite)}
          as={linkComponent}
          className={AUTH_LINK_CLASS}
        >
          {t.register}
        </AuthLink>
      </p>
    </div>
  )
}

// The heading lives in AuthLayout, above the card, so the screen — not the
// form — passes the copy. Exposing the defaults here keeps the wording in one
// place: `<AuthLayout {...LoginForm.defaults}>` renders what the form used to.
LoginForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
