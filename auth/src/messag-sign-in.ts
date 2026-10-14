export const MESSAG_SIGN_IN_PATH = "/api/auth/sign-in/messag"
export const MESSAG_DOMAIN = "messag.eco"
export const MESSAG_SIGN_UP_URL = "https://messag.eco/register"
export const INVITATION_REQUIRED = "invitation_required"

export function messagSignInUrl(callbackURL?: string): string {
  if (!callbackURL) return MESSAG_SIGN_IN_PATH
  return `${MESSAG_SIGN_IN_PATH}?${new URLSearchParams({ callbackURL })}`
}

export function isMessagAddress(email: string, domain = MESSAG_DOMAIN): boolean {
  return email.trim().toLowerCase().endsWith(`@${domain.toLowerCase()}`)
}

export function messagRegisterUrl(
  returnTo: string,
  base = MESSAG_SIGN_UP_URL,
): string {
  return `${base}?${new URLSearchParams({ return: returnTo })}`
}

export function isInvitationRequired(
  error: { code?: string } | null | undefined,
): boolean {
  return error?.code === INVITATION_REQUIRED
}

/**
 * A "Se connecter avec messag" refused on invitation comes back to the
 * sign-in page as `?error=invitation_required`, with no component mounted to
 * have caught it. Read once, from the address bar.
 */
export function initialInvitationRefusal(param = "error"): boolean {
  if (typeof window === "undefined") return false
  return (
    new URLSearchParams(window.location.search).get(param) ===
    INVITATION_REQUIRED
  )
}
