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
