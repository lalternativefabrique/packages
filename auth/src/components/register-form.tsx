import { useState, type FormEvent } from "react"
import type { RegisterFormLabels, RegisterFormProps } from "../types"
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

  const tooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH

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
    <div className="space-y-7">
      <header className="space-y-1.5">
        <h1 className="text-[26px] font-semibold leading-tight tracking-[-0.02em] text-foreground sm:text-2xl">
          {t.title}
        </h1>
        <p className="text-sm leading-relaxed text-muted-foreground">
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
          label={t.namePlaceholder}
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          disabled={isPending}
          autoComplete="name"
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
        />

        <AuthSubmit pending={isPending} pendingLabel={t.submitPending}>
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
