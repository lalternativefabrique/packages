import { useState, type FormEvent } from "react"
import type { MagicLinkFormLabels, MagicLinkFormProps } from "../types"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { AUTH_LINK_CLASS, AuthLink } from "./auth-link"
import { withInviteToken } from "../invite-token"
import { magicLinkErrorCallback } from "../magic-link-error"

/**
 * Sign-in by emailed link.
 *
 * The confirmation says a link was sent, never that an account exists: Better
 * Auth answers the send the same way either way, and only refuses at the
 * /magic-link/verify hop. That is deliberate on its part — a form that
 * distinguished the two would tell an anonymous caller which addresses are
 * registered. Every failure past the send therefore comes back as a `?error=`
 * on the errorCallbackURL, which is what magicLinkErrorMessage reads — and
 * which defaults to this page, since asking for another link is the fix.
 */

const DEFAULTS: Required<MagicLinkFormLabels> = {
  title: "Connexion par lien",
  subtitle:
    "Entre ton adresse e-mail et nous t'enverrons un lien pour te connecter, sans mot de passe.",
  emailPlaceholder: "Adresse e-mail",
  submit: "Envoyer le lien",
  submitPending: "Envoi…",
  sent: "Lien envoyé. Ouvre ta boîte de réception pour te connecter — il expire dans 5 minutes.",
  resend: "Renvoyer le lien",
  usePassword: "Tu préfères ton mot de passe ?",
  login: "Se connecter",
  emailRequired: "Renseigne ton adresse e-mail",
  sendFailed: "L'envoi du lien a échoué",
}

export function MagicLinkForm({
  onSuccess,
  loginUrl = "/login",
  callbackUrl = "/",
  newUserCallbackUrl,
  errorCallbackUrl,
  labels,
  submitClassName,
  fieldClassName,
  error: externalError,
  linkComponent,
  invite,
  authClient,
}: MagicLinkFormProps) {
  const t = { ...DEFAULTS, ...labels }
  const [email, setEmail] = useState("")
  const [ownError, setOwnError] = useState<string | undefined>()
  const [isPending, setIsPending] = useState(false)
  const [isSent, setIsSent] = useState(false)

  const error = ownError ?? externalError

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim()) {
      setOwnError(t.emailRequired)
      return
    }
    setOwnError(undefined)
    setIsPending(true)
    try {
      const res = await authClient.signIn.magicLink({
        email: email.trim(),
        // The invitation rides the callback: following the link leaves the
        // browser on the mail client, so the auth handler redeems the token on
        // the way back, as it does for OAuth.
        callbackURL: withInviteToken(callbackUrl, invite),
        ...(newUserCallbackUrl
          ? { newUserCallbackURL: withInviteToken(newUserCallbackUrl, invite) }
          : {}),
        // Resolved here rather than at render: the default is the current page,
        // and this runs in the browser, where there is one.
        errorCallbackURL: withInviteToken(
          magicLinkErrorCallback(
            errorCallbackUrl,
            loginUrl,
            typeof window !== "undefined"
              ? window.location.pathname
              : undefined,
          ),
          invite,
        ),
      })
      if (res?.error) {
        setOwnError(res.error.message ?? t.sendFailed)
        return
      }
      setIsSent(true)
      onSuccess?.(email.trim())
    } catch (err) {
      setOwnError(err instanceof Error ? err.message : t.sendFailed)
    } finally {
      setIsPending(false)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>
      {/* Kept out of the error alert's node so the two live regions stay
          distinct — a success replacing an error in the same node is announced
          as a change to the error. */}
      <AuthAlert tone="success">{!error && isSent ? t.sent : undefined}</AuthAlert>

      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthField
          label={t.emailPlaceholder}
          type="email"
          inputMode="email"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value)
            // Editing the address invalidates what was said about the last
            // one: the confirmation named an inbox this is no longer it.
            setIsSent(false)
          }}
          required
          disabled={isPending}
          autoComplete="email"
          autoCapitalize="none"
          spellCheck={false}
          invalid={!!error}
          fieldClassName={fieldClassName}
        />

        <AuthSubmit
          spacedAbove
          pending={isPending}
          disabled={!email.trim()}
          pendingLabel={t.submitPending}
          className={submitClassName}
        >
          {isSent ? t.resend : t.submit}
        </AuthSubmit>
      </form>

      <p className="text-center text-sm text-muted-foreground">
        {t.usePassword}{" "}
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
MagicLinkForm.defaults = {
  title: DEFAULTS.title,
  subtitle: DEFAULTS.subtitle,
}
