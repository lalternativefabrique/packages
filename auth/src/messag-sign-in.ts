export interface MessagSignInConfig {
  /** Messag's web, where an address is created */
  url?: string
  /** The mail domain of a Messag address */
  domain?: string
}

export const MESSAG_DEFAULTS: Required<MessagSignInConfig> = {
  url: "https://messag.eco",
  domain: "messag.eco",
}

const LOCAL_PART = /^[a-z0-9]+(?:[._-][a-z0-9]+)*$/

export function messagAddress(typed: string, domain: string): string | null {
  const value = typed.trim().toLowerCase()
  const suffix = `@${domain.toLowerCase()}`
  const local = value.endsWith(suffix) ? value.slice(0, -suffix.length) : value
  return LOCAL_PART.test(local) ? `${local}${suffix}` : null
}

export function messagLocalPart(
  email: string | undefined,
  domain: string,
): string | undefined {
  const value = email?.trim().toLowerCase()
  const suffix = `@${domain.toLowerCase()}`
  if (!value?.endsWith(suffix)) return undefined
  return value.slice(0, -suffix.length) || undefined
}

/**
 * Messag's sign-up hands the created address back to `returnTo` as
 * `?email=`, and only to an origin on its own allowlist.
 */
export function messagRegisterUrl(url: string, returnTo?: string): string {
  const register = `${url.replace(/\/+$/, "")}/register`
  if (!returnTo) return register
  const back = new URL(returnTo)
  back.searchParams.delete("email")
  return `${register}?return=${encodeURIComponent(back.toString())}`
}
