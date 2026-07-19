import type { UserProfile } from "../types"

/**
 * The single admin-role policy point, shared by every app's route guard and
 * admin UI. Widen the rule here (e.g. a dedicated role) rather than at call
 * sites.
 */
export function hasAdminFeatures(profile: UserProfile | undefined | null): boolean {
  return profile?.roles?.includes("admin") ?? false
}

/**
 * Resolves the admin flag from a profile you already have (e.g. a cached
 * `['me']` query). Kept dependency-free — no react-query, no auth client — so
 * the package stays portable; the app owns how the profile is fetched.
 */
export function isAdminProfile(profile: UserProfile | undefined | null): boolean {
  return hasAdminFeatures(profile)
}
