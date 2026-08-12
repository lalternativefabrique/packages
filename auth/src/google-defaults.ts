import type { PlatformAuthConfig } from "./types"

/**
 * Applies the platform's Google defaults without touching what the app set.
 *
 * Better Auth also accepts a function returning the options, which cannot be
 * amended without calling it — such a config is passed straight through and
 * owns its own defaults.
 */
export function withGoogleDefaults(
  google: NonNullable<PlatformAuthConfig["google"]>,
): NonNullable<PlatformAuthConfig["google"]> {
  if (typeof google === "function") return google
  return {
    ...google,
    accessType: google.accessType ?? "offline",
    prompt: google.prompt ?? "select_account consent",
  }
}

