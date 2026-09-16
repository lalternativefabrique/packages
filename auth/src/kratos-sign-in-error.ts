import type { AuthClientResult } from "./types"

/**
 * Whether the sign-in was refused because the person's identity carries no
 * password yet at the provider — an account that predates the move, whose
 * owner sets a password once through the recovery flow.
 *
 * It is not a wrong password, and rendering it as one sends the person
 * retrying an old password that will never work again.
 */
export function needsPasswordRecovery(
  error: AuthClientResult["error"],
): boolean {
  return error?.code === "IDENTITY_HAS_NO_PASSWORD"
}

/**
 * Whether the identity provider could not be reached. The password was never
 * refused: the person retries, and must not be told to change it.
 */
export function isIdentityProviderUnavailable(
  error: AuthClientResult["error"],
): boolean {
  return (
    error?.code === "IDENTITY_PROVIDER_UNAVAILABLE" || error?.status === 503
  )
}

export function needsSecondFactor(error: AuthClientResult["error"]): boolean {
  return error?.code === "SECOND_FACTOR_REQUIRED"
}

export function isAccountDisabled(error: AuthClientResult["error"]): boolean {
  return error?.code === "ACCOUNT_DISABLED"
}
