export const MESSAG_SIGN_IN_PATH = "/api/auth/sign-in/messag"

export function messagSignInUrl(callbackURL?: string): string {
  if (!callbackURL) return MESSAG_SIGN_IN_PATH
  return `${MESSAG_SIGN_IN_PATH}?${new URLSearchParams({ callbackURL })}`
}
