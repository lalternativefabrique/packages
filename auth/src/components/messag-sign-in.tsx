import { useState, type FormEvent } from "react"
import type { AuthClientSurface } from "../types"
import {
  MESSAG_DEFAULTS,
  messagAddress,
  messagLocalPart,
  messagRegisterUrl,
  type MessagSignInConfig,
} from "../messag-sign-in"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { AUTH_LINK_CLASS } from "./auth-link"

export interface MessagSignInLabels {
  button?: string
  address?: string
  password?: string
  submit?: string
  submitPending?: string
  noAddress?: string
  createAddress?: string
  addressInvalid?: string
  passwordRequired?: string
  invalidCredentials?: string
}

const DEFAULTS: Required<MessagSignInLabels> = {
  button: "Se connecter avec messag",
  address: "Adresse messag",
  password: "Mot de passe messag",
  submit: "Se connecter",
  submitPending: "Connexion…",
  noAddress: "Pas encore d'adresse messag ?",
  createAddress: "Crée-la",
  addressInvalid: "Renseigne ton adresse messag",
  passwordRequired: "Renseigne ton mot de passe",
  invalidCredentials: "Adresse messag ou mot de passe incorrect",
}

export interface MessagSignInProps extends MessagSignInConfig {
  authClient: AuthClientSurface
  onSuccess?: () => void
  /** An address Messag's sign-up handed back opens the form prefilled */
  defaultEmail?: string
  /**
   * The page Messag's sign-up sends the new address back to. Defaults to the
   * current page, which must be on Messag's list of suite origins.
   */
  returnUrl?: string
  coreTokenUrl?: string | null
  disabled?: boolean
  labels?: MessagSignInLabels
  fieldClassName?: string
  submitClassName?: string
}

/**
 * A Messag account is a suite identity whose address is on Messag's domain
 * (urbangate ADR 0016), so signing in with it is the product's own password
 * sign-in, and creating one happens on Messag.
 */
export function MessagSignIn({
  authClient,
  onSuccess,
  defaultEmail,
  returnUrl,
  coreTokenUrl = "/api/auth/core-token",
  disabled = false,
  labels,
  fieldClassName,
  submitClassName,
  url = MESSAG_DEFAULTS.url,
  domain = MESSAG_DEFAULTS.domain,
}: MessagSignInProps) {
  const t = { ...DEFAULTS, ...labels }
  const returned = messagLocalPart(defaultEmail, domain)
  const [open, setOpen] = useState(returned !== undefined)
  const [local, setLocal] = useState(returned ?? "")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [pending, setPending] = useState(false)

  const register = () =>
    messagRegisterUrl(
      url,
      returnUrl ??
        (typeof window !== "undefined" ? window.location.href : undefined),
    )

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const email = messagAddress(local, domain)
    if (!email) {
      setError(t.addressInvalid)
      return
    }
    if (!password) {
      setError(t.passwordRequired)
      return
    }
    setError(undefined)
    setPending(true)
    try {
      const res = await authClient.signIn.email({ email, password })
      if (res?.error) {
        setError(res.error.message ?? t.invalidCredentials)
        return
      }
      if (coreTokenUrl) {
        await fetch(coreTokenUrl, { credentials: "include" })
      }
      onSuccess?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : t.invalidCredentials)
    } finally {
      setPending(false)
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={disabled}
        className={[
          "flex h-[52px] w-full items-center justify-center gap-2.5 rounded-lg sm:h-11",
          "border border-foreground/25 bg-background text-base font-medium text-foreground sm:text-sm",
          "transition-[background-color,border-color,transform] duration-150 ease-out",
          "hover:border-foreground/20 hover:bg-accent active:translate-y-px",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
          "disabled:pointer-events-none disabled:opacity-55",
        ].join(" ")}
      >
        <MessagMark />
        {t.button}
      </button>
    )
  }

  return (
    <div className="space-y-4">
      <AuthAlert>{error}</AuthAlert>
      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthField
          label={t.address}
          type="text"
          inputMode="email"
          value={local}
          onChange={(e) => setLocal(e.target.value)}
          placeholder={`identifiant@${domain}`}
          required
          disabled={pending || disabled}
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          autoFocus={returned === undefined}
          invalid={!!error}
          fieldClassName={fieldClassName}
        />
        <AuthField
          label={t.password}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          disabled={pending || disabled}
          autoComplete="current-password"
          autoFocus={returned !== undefined}
          invalid={!!error}
          fieldClassName={fieldClassName}
        />
        <AuthSubmit
          spacedAbove
          pending={pending}
          disabled={!local.trim() || !password || disabled}
          pendingLabel={t.submitPending}
          className={submitClassName}
        >
          {t.submit}
        </AuthSubmit>
      </form>
      <p className="text-center text-sm text-muted-foreground">
        {t.noAddress}{" "}
        <a
          href={register()}
          className={AUTH_LINK_CLASS}
          onClick={(e) => {
            e.currentTarget.href = register()
          }}
        >
          {t.createAddress}
        </a>
      </p>
    </div>
  )
}

function MessagMark() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="2" y="4" width="20" height="16" rx="2" />
      <path d="m22 7-10 6L2 7" />
    </svg>
  )
}
