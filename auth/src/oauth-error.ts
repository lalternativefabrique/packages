export type OAuthErrorLabels = {
  accountNotLinked: string
  socialCancelled: string
  socialFailed: string
}

/**
 * Turns Better Auth's machine-readable OAuth failure code into something an
 * invitee can act on.
 *
 * `account_not_linked` is the one worth naming: createPlatformAuth disables
 * account linking on purpose, so "Continue with Google" on an address already
 * registered with a password is REFUSED. Better Auth's default error route
 * bounces the browser back with nothing shown, so without this the button
 * simply looks broken.
 */
export function oauthErrorMessage(
  code: string | null | undefined,
  labels: OAuthErrorLabels,
): string {
  switch (code) {
    case "account_not_linked":
      return labels.accountNotLinked
    case "access_denied":
      return labels.socialCancelled
    default:
      return labels.socialFailed
  }
}

/**
 * Reads the failure Better Auth redirected back with.
 *
 * A social sign-in leaves the app entirely, so component state does not survive
 * it: the only carrier left when the browser returns is the `?error=` Better
 * Auth appends to the errorCallbackURL. Read from the address bar rather than
 * from a typed route search, because the auth routes deliberately declare no
 * search schema — adding one would make `search` required on every navigate to
 * them across the app.
 */
export function initialOAuthError(
  labels: OAuthErrorLabels,
  param = "error",
): string | undefined {
  if (typeof window === "undefined") return undefined
  const code = new URLSearchParams(window.location.search).get(param)
  if (!code) return undefined
  return oauthErrorMessage(code, labels)
}

/**
 * Where a refused social sign-in should send the browser back to.
 *
 * Falls back to the page the form is on, which is the one that reads `?error=`
 * and renders it. Better Auth would otherwise keep the browser on its own
 * error route, where nothing shows the code and the button reads as broken.
 * `fallback` covers the server render, where there is no current page to name.
 */
export function oauthErrorCallback(
  explicit: string | undefined,
  fallback: string,
  currentPath: string | undefined,
): string {
  return explicit ?? currentPath ?? fallback
}

/**
 * Drops the failure from the address bar once it has been shown.
 *
 * Without this the message comes back on every reload, and outlives the retry
 * that succeeded. replaceState rather than a router navigate: these screens
 * have no typed search to navigate against, and the entry being rewritten is
 * the one the OAuth provider pushed, not one the person chose.
 */
export function clearOAuthError(param = "error"): void {
  if (typeof window === "undefined") return
  const url = new URL(window.location.href)
  if (!url.searchParams.has(param)) return
  url.searchParams.delete(param)
  window.history.replaceState({}, "", url.toString())
}
