import type { SsoClientSurface } from "./types"

/**
 * Starts the redirect to a generic OAuth provider mounted by createPlatformAuth
 * (the suite's identity provider, providerId "urbangate" by default). Better
 * Auth 1.7 serves generic providers through the social sign-in endpoint, whose
 * provider type only names the built-in ones.
 */
export function startSso(
  client: Pick<SsoClientSurface, "signIn">,
  args: { providerId?: string; callbackURL?: string; errorCallbackURL?: string },
): Promise<unknown> {
  return client.signIn.social({
    provider: args.providerId ?? "urbangate",
    callbackURL: args.callbackURL,
    errorCallbackURL: args.errorCallbackURL,
  })
}
