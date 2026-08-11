/**
 * An invitation token as it may appear in a URL, reduced to something safe to
 * carry through a route search schema.
 *
 * Callers pass raw search params (`Route.validateSearch`), so the input is
 * whatever the address bar held: an array when the param repeats, a number, or
 * a string long enough to be an attack rather than a token. Anything that is
 * not one plausible token collapses to undefined, which reads as "no
 * invitation" everywhere downstream.
 */
const MAX_TOKEN_LENGTH = 128

export function normalizeInviteToken(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const trimmed = value.trim()
  if (!trimmed || trimmed.length > MAX_TOKEN_LENGTH) return undefined
  return trimmed
}

/**
 * Appends the invitation to a link between auth screens.
 *
 * An invitee who lands on /register and clicks through to /login must keep the
 * offer: the token lives only in the URL, so a plain href to the sibling screen
 * silently drops it and the account is created on the default tier.
 */
export function withInviteToken(href: string, token?: string): string {
  if (!token) return href
  const separator = href.includes("?") ? "&" : "?"
  return `${href}${separator}invite=${encodeURIComponent(token)}`
}
