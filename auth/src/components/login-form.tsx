import { useState, type FormEvent } from "react"
import type { LoginFormLabels, LoginFormProps } from "../types"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { SocialButtons } from "./social-buttons"

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
  registerUrl = "/register",
  forgotPasswordUrl = "/forgot-password",
  socialCallbackUrl = "/",
  socialProviders = [],
  coreTokenUrl = "/api/auth/core-token",
  labels,
  authClient,
}: LoginFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

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
        setError(res.error.message ?? t.invalidCredentials)
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
        callbackURL: socialCallbackUrl,
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
    <div className="space-y-7">
      <header className="space-y-1.5">
        <h1 className="text-[26px] font-semibold leading-tight tracking-[-0.02em] text-foreground sm:text-2xl">
          {t.title}
        </h1>
        <p className="text-balance text-sm leading-relaxed text-muted-foreground">
          {t.subtitle}
        </p>
      </header>

      {error && (
        <div
          role="alert"
          className="rounded-xl border border-destructive/25 bg-destructive/10 px-3.5 py-3 text-sm leading-snug text-destructive"
        >
          {error}
        </div>
      )}

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

        <AuthField
          label={t.passwordPlaceholder}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          disabled={isPending}
          autoComplete="current-password"
          invalid={!!error}
          hint={
            <a
              href={forgotPasswordUrl}
              className="rounded text-xs text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t.forgotPassword}
            </a>
          }
        />

        <AuthSubmit
          pending={isPending}
          disabled={!email.trim() || !password}
          pendingLabel={t.submitPending}
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
        <a
          href={registerUrl}
          className="rounded font-medium text-foreground underline underline-offset-4 decoration-foreground/25 transition-colors hover:decoration-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t.register}
        </a>
      </p>
    </div>
  )
}
