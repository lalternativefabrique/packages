/**
 * Where a link that did not work should send the browser back to.
 *
 * Falls back to the current page rather than to Better Auth's own default,
 * which is the success callback: that is a signed-in destination, so an auth
 * guard bounces the visitor and drops the `?error=` on the way, and an expired
 * link ends up looking like nothing happened. `fallback` covers the server
 * render, where there is no current page to name.
 */
export function magicLinkErrorCallback(
  explicit: string | undefined,
  fallback: string,
  currentPath: string | undefined,
): string {
  return explicit ?? currentPath ?? fallback
}

export type MagicLinkErrorLabels = {
  invalidToken: string
  signUpDisabled: string
  failed: string
}

export const MAGIC_LINK_ERROR_DEFAULTS: MagicLinkErrorLabels = {
  invalidToken:
    "Ce lien n'est plus valide. Il expire après 5 minutes et ne fonctionne qu'une fois — demandes-en un nouveau.",
  signUpDisabled:
    "Aucun compte n'existe pour cette adresse. Crée-en un d'abord.",
  failed: "La connexion par lien a échoué. Réessaie.",
}

/**
 * Turns the failure a followed magic link redirected back with into something
 * the person can act on.
 *
 * Every way the flow can fail lands here rather than on the send: Better Auth
 * consumes the token at /magic-link/verify, and any refusal there is thrown as
 * a redirect to the errorCallbackURL. `INVALID_TOKEN` covers expiry and reuse
 * alike — the token is consumed atomically on first use, so a link followed
 * twice is indistinguishable from one that timed out, and the copy names both.
 */
export function magicLinkErrorMessage(
  code: string | null | undefined,
  labels: Partial<MagicLinkErrorLabels> = {},
): string {
  const t = { ...MAGIC_LINK_ERROR_DEFAULTS, ...labels }
  switch (code) {
    case "INVALID_TOKEN":
      return t.invalidToken
    case "new_user_signup_disabled":
      return t.signUpDisabled
    default:
      return t.failed
  }
}

/** Codes this module recognises, so a shared handler can tell them apart. */
const MAGIC_LINK_ERROR_CODES = new Set([
  "INVALID_TOKEN",
  "new_user_signup_disabled",
  "failed_to_create_user",
  "failed_to_create_session",
])

/**
 * Whether a `?error=` came from a magic link rather than from OAuth.
 *
 * Both flows return to the same screens through the same parameter, so a page
 * offering the two needs to know which vocabulary to read the code against —
 * otherwise an expired link is reported as a failed social sign-in.
 */
export function isMagicLinkError(code: string | null | undefined): boolean {
  return !!code && MAGIC_LINK_ERROR_CODES.has(code)
}

/**
 * Reads the failure a followed link redirected back with.
 *
 * Following a link leaves the app entirely, so no component state survives it;
 * the address bar is the only carrier left. Mirrors initialOAuthError, down to
 * reading `window.location` rather than a typed route search — the auth routes
 * declare none on purpose.
 */
export function initialMagicLinkError(
  labels: Partial<MagicLinkErrorLabels> = {},
  param = "error",
): string | undefined {
  if (typeof window === "undefined") return undefined
  const code = new URLSearchParams(window.location.search).get(param)
  if (!isMagicLinkError(code)) return undefined
  return magicLinkErrorMessage(code, labels)
}
