import { useState, type FormEvent } from "react"
import type { LoginFormLabels, LoginFormProps } from "../types"
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
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t.title}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t.subtitle}</p>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-4" noValidate>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder={t.emailPlaceholder}
          aria-label={t.emailPlaceholder}
          required
          disabled={isPending}
          autoComplete="email"
          className="flex h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 disabled:cursor-not-allowed"
        />

        <div className="space-y-1">
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t.passwordPlaceholder}
            aria-label={t.passwordPlaceholder}
            required
            disabled={isPending}
            autoComplete="current-password"
            className="flex h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 disabled:cursor-not-allowed"
          />
          <div className="text-right">
            <a
              href={forgotPasswordUrl}
              className="text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
            >
              {t.forgotPassword}
            </a>
          </div>
        </div>

        <button
          type="submit"
          disabled={isPending || !email.trim() || !password}
          className="inline-flex h-11 w-full items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {isPending ? t.submitPending : t.submit}
        </button>
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
          className="font-medium text-foreground underline underline-offset-4 hover:text-foreground/80"
        >
          {t.register}
        </a>
      </p>
    </div>
  )
}
