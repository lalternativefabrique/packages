export interface SsoProfile {
  sub?: string
  email?: string
  email_verified?: boolean
  name?: string
  picture?: string
  roles?: unknown
}

export interface SsoMappedUser {
  [key: string]: unknown
  email: string
  emailVerified: boolean
  name: string
  image?: string
  role: "admin" | "user"
}

export function rolesOf(profile: SsoProfile): string[] {
  return Array.isArray(profile.roles)
    ? profile.roles.filter((r): r is string => typeof r === "string")
    : []
}

/**
 * Maps the identity provider's claims onto the local user. The admin role is
 * recomputed from the roles claim on every sign-in, so a role removed at the
 * provider is removed here the next time the person signs in.
 */
export function mapSsoProfile(profile: SsoProfile, adminRole: string): SsoMappedUser {
  const email = (profile.email ?? "").trim().toLowerCase()
  return {
    email,
    emailVerified: profile.email_verified === true,
    name: profile.name?.trim() || email.split("@")[0] || "",
    ...(profile.picture ? { image: profile.picture } : {}),
    role: rolesOf(profile).includes(adminRole) ? "admin" : "user",
  }
}

function decodeJwtPayload(token: string): Record<string, unknown> | undefined {
  const part = token.split(".")[1]
  if (!part) return undefined
  try {
    const binary = atob(part.replace(/-/g, "+").replace(/_/g, "/"))
    const json = new TextDecoder().decode(Uint8Array.from(binary, (c) => c.charCodeAt(0)))
    const parsed: unknown = JSON.parse(json)
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : undefined
  } catch {
    return undefined
  }
}

/**
 * The local role an ID token from the identity provider grants, or undefined
 * when the token cannot be read, so a broken token never demotes anyone by
 * accident. Roles are read from the top-level claim or from Hydra's `ext`.
 */
export function roleFromIdToken(idToken: string | null | undefined, adminRole: string): "admin" | "user" | undefined {
  if (!idToken) return undefined
  const claims = decodeJwtPayload(idToken)
  if (!claims) return undefined
  const ext = claims.ext
  const raw = Array.isArray(claims.roles)
    ? claims.roles
    : ext && typeof ext === "object" && Array.isArray((ext as { roles?: unknown }).roles)
      ? (ext as { roles: unknown[] }).roles
      : []
  return raw.includes(adminRole) ? "admin" : "user"
}
