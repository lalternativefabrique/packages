import { useState, type FormEvent } from "react"
import type { LoginFormProps } from "../types"
import { SocialButtons } from "./social-buttons"

// accountLinking is disabled in createPlatformAuth, so a social sign-in on an
// address already registered with a password is refused with this code. Without
// a message the button reads as broken rather than as a rejected account.
function oauthErrorMessage(code: string): string {
  switch (code) {
    case "account_not_linked":
      return "This email is already registered with a password. Sign in with your password instead."
    case "access_denied":
      return "Sign-in was cancelled."
    default:
      return "Sign-in failed. Please try again."
  }
}

export function LoginForm({
  onSuccess,
  registerUrl = "/register",
  forgotPasswordUrl = "/forgot-password",
  socialCallbackUrl = "/",
  socialProviders = [],
  coreTokenUrl = "/api/auth/core-token",
  authClient,
}: LoginFormProps) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim()) {
      setError("Please enter your email address")
      return
    }
    if (!password) {
      setError("Please enter your password")
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
        setError(res.error.message ?? "Invalid email or password")
        return
      }
      // The better-auth session cookie alone does not authenticate the Go core:
      // it verifies the EdDSA JWT minted here against the issuer's JWKS. Skipped
      // when the app has no core to call.
      if (coreTokenUrl) {
        await fetch(coreTokenUrl, { credentials: "include" })
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Invalid email or password")
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
      setError(
        err instanceof Error ? oauthErrorMessage(err.message) : oauthErrorMessage(""),
      )
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Sign in</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Welcome back. Enter your credentials to continue.
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-4" noValidate>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="Email address"
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
            placeholder="Password"
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
              Forgot your password?
            </a>
          </div>
        </div>

        <button
          type="submit"
          disabled={isPending || !email.trim() || !password}
          className="inline-flex h-11 w-full items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {isPending ? "Signing in..." : "Sign in"}
        </button>
      </form>

      <SocialButtons
        providers={socialProviders}
        onSelect={handleSocial}
        disabled={isPending}
      />

      <p className="text-center text-sm text-muted-foreground">
        Don&apos;t have an account?{" "}
        <a
          href={registerUrl}
          className="font-medium text-foreground underline underline-offset-4 hover:text-foreground/80"
        >
          Sign up
        </a>
      </p>
    </div>
  )
}
