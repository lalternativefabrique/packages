import { createAuthClient } from "better-auth/react"
import {
  emailOTPClient,
  adminClient,
  magicLinkClient,
  twoFactorClient,
} from "better-auth/client/plugins"
import type {
  AdminClientSurface,
  AuthClientSurface,
  MagicLinkClientSurface,
  PlatformAuthClientConfig,
  SsoClientSurface,
  TwoFactorClientSurface,
} from "./types"

/**
 * A Better Auth React client carrying the platform plugins.
 *
 * The concrete inferred type cannot be named in a published .d.ts (TS2742 — it
 * reaches into zod's internals), so the surface the auth screens call is
 * declared by hand in AuthClientSurface and intersected with the rest of the
 * client. Keep it in sync with the plugins enabled below.
 *
 * signIn carries both halves: the client always mounts magicLinkClient, since
 * which methods exist client-side costs nothing — whether the route answers is
 * decided server-side by passing `magicLink` to createPlatformAuth.
 *
 * admin is optional on AuthClientSurface, whose job is to type the prop the
 * forms take, and required here: this client always mounts adminClient(), so a
 * back-office calling admin.listUsers() off it must not have to widen the type.
 */
export type PlatformAuthClient = Omit<AuthClientSurface, "signIn" | "admin"> & {
  signIn: AuthClientSurface["signIn"] &
    MagicLinkClientSurface["signIn"] &
    SsoClientSurface["signIn"]
  admin: AdminClientSurface
  twoFactor: TwoFactorClientSurface
} & Omit<
    ReturnType<typeof createAuthClient>,
    | keyof AuthClientSurface
    | keyof MagicLinkClientSurface
    | keyof SsoClientSurface
    | "twoFactor"
  >

/**
 * Creates a Better Auth client for React usage.
 * Provides useSession() and the email-OTP / magic-link / admin plugin methods.
 */
export function createPlatformAuthClient(
  config?: PlatformAuthClientConfig,
): PlatformAuthClient {
  return createAuthClient({
    baseURL:
      config?.baseURL ??
      (typeof window !== "undefined"
        ? window.location.origin
        : "http://localhost:3000"),
    plugins: [
      emailOTPClient(),
      magicLinkClient(),
      adminClient(),
      twoFactorClient(),
      ...(config?.plugins ?? []),
    ],
  }) as unknown as PlatformAuthClient
}

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
