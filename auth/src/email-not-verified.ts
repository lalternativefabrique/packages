import type { AuthClientResult } from "./types"

/**
 * Whether a failed sign-in was refused for want of a confirmed address.
 *
 * Better Auth answers an unverified sign-in with EMAIL_NOT_VERIFIED, but only
 * when built with its error codes exposed; otherwise the refusal arrives as a
 * bare 403. No other credential failure on this route uses that status — a
 * wrong password is a 401 — so the status alone is a safe fallback.
 */
export function isEmailNotVerified(
  error: AuthClientResult["error"],
): boolean {
  if (!error) return false
  return error.code === "EMAIL_NOT_VERIFIED" || error.status === 403
}
