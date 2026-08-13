import { createAuthClient } from "better-auth/react"
import {
  emailOTPClient,
  adminClient,
  magicLinkClient,
} from "better-auth/client/plugins"
import type {
  AdminClientSurface,
  AuthClientSurface,
  MagicLinkClientSurface,
  PlatformAuthClientConfig,
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
  signIn: AuthClientSurface["signIn"] & MagicLinkClientSurface["signIn"]
  admin: AdminClientSurface
} & Omit<
    ReturnType<typeof createAuthClient>,
    keyof AuthClientSurface | keyof MagicLinkClientSurface
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
      ...(config?.plugins ?? []),
    ],
  }) as unknown as PlatformAuthClient
}
