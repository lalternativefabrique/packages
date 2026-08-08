import { useState, type FormEvent } from "react"
import type { RegisterFormLabels, RegisterFormProps } from "../types"
import { SocialButtons } from "./social-buttons"

const MIN_PASSWORD_LENGTH = 8

const DEFAULTS: Required<RegisterFormLabels> = {
  title: "Créer un compte",
  subtitle: "Nous t'enverrons un code pour confirmer ton adresse e-mail.",
  namePlaceholder: "Nom complet",
  emailPlaceholder: "Adresse e-mail",
  passwordPlaceholder: "Mot de passe",
  passwordHint: `Au moins ${MIN_PASSWORD_LENGTH} caractères.`,
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
  authClient,
}: RegisterFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [name, setName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

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
        callbackURL: socialCallbackUrl,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : t.signUpFailed)
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
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t.namePlaceholder}
          aria-label={t.namePlaceholder}
          required
          disabled={isPending}
          autoComplete="name"
          className="flex h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 disabled:cursor-not-allowed"
        />

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
            autoComplete="new-password"
            aria-describedby="register-password-hint"
            className="flex h-11 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50 disabled:cursor-not-allowed"
          />
          <p id="register-password-hint" className="text-xs text-muted-foreground">
            {t.passwordHint}
          </p>
        </div>

        <button
          type="submit"
          disabled={isPending}
          className="inline-flex h-11 w-full items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {isPending ? t.submitPending : t.submit}
        </button>

        {legal && (
          <div className="text-center text-xs text-muted-foreground">{legal}</div>
        )}
      </form>

      <SocialButtons
        providers={socialProviders}
        onSelect={handleSocial}
        disabled={isPending}
      />

      <p className="text-center text-sm text-muted-foreground">
        {t.haveAccount}{" "}
        <a
          href={loginUrl}
          className="font-medium text-foreground underline underline-offset-4 hover:text-foreground/80"
        >
          {t.login}
        </a>
      </p>
    </div>
  )
}
