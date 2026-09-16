import { betterAuth, APIError, type Auth, type BetterAuthOptions } from "better-auth"
import { emailOTP, admin, magicLink, twoFactor, genericOAuth } from "better-auth/plugins"
import type {
  PlatformAuthConfig,
  PlatformAuthMailerType,
  PlatformKratosPasswordConfig,
  PlatformSsoConfig,
} from "./types"
import {
  KRATOS_SENTINEL_HASH,
  verifyAgainstKratos,
  type KratosOutcome,
} from "./kratos-credentials"
import { provisionIdentity } from "./identity-provisioning"
import { withGoogleDefaults } from "./google-defaults"
import { mapSsoProfile, roleFromIdToken, type SsoProfile } from "./sso-profile"
import { ssoEndpoints } from "./sso-endpoints"
import { withSignUpName } from "./signup-name"
import { resolveRateLimit } from "./rate-limit"

const DEFAULT_EMAIL_SUBJECTS: Record<string, string> = {
  "email-verification": "Verify your account",
  "forget-password": "Reset your password",
  "sign-in": "Your sign-in code",
}

function defaultRenderOtpEmail(otp: string): string {
  return `
              <div style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px">
                <h2 style="font-size:20px;font-weight:600;margin-bottom:16px">Your verification code</h2>
                <p style="color:#555;margin-bottom:24px">Use the code below to continue. It expires in 5 minutes.</p>
                <div style="background:#f5f5f5;border-radius:8px;padding:24px;text-align:center;letter-spacing:8px;font-size:32px;font-weight:700">
                  ${otp}
                </div>
                <p style="color:#999;font-size:12px;margin-top:24px">If you didn't request this, you can safely ignore this email.</p>
              </div>
            `
}

function defaultRenderMagicLinkEmail(url: string): string {
  return `
              <div style="font-family:sans-serif;max-width:480px;margin:0 auto;padding:32px">
                <h2 style="font-size:20px;font-weight:600;margin-bottom:16px">Your sign-in link</h2>
                <p style="color:#555;margin-bottom:24px">Click the button below to sign in. The link expires in 5 minutes and works once.</p>
                <a href="${url}" style="display:inline-block;background:#111;color:#fff;text-decoration:none;border-radius:8px;padding:14px 28px;font-weight:600">Sign in</a>
                <p style="color:#999;font-size:12px;margin-top:24px;word-break:break-all">Or paste this address into your browser:<br>${url}</p>
                <p style="color:#999;font-size:12px;margin-top:16px">If you didn't request this, you can safely ignore this email.</p>
              </div>
            `
}

/**
 * Creates a Better Auth instance with platform defaults.
 * Each app calls this with its own config (DB, secret, providers, plugins).
 */
export function createPlatformAuth(
  config: PlatformAuthConfig,
): Auth<BetterAuthOptions> {
  const {
    database,
    baseURL,
    secret,
    appName,
    mailer,
    google,
    github,
    plugins = [],
    databaseHooks,
    betaMode = false,
    isInvited,
    emailSubjects,
    renderOtpEmail,
    magicLink: magicLinkConfig,
    rateLimit,
    twoFactor: twoFactorConfig,
    trustedOrigins,
    sso,
    kratosPasswords,
  } = config
  const ssoProviderId = sso?.providerId ?? "urbangate"

  const subjects = { ...DEFAULT_EMAIL_SUBJECTS, ...emailSubjects }
  const renderEmail = renderOtpEmail ?? defaultRenderOtpEmail

  // The concrete instance type (with email-otp/admin plugins) is widened to
  // the base Auth type so the published .d.ts stays portable (inferring the
  // full plugin type triggers TS2742 — it can't be named without a zod ref).
  // The admin() plugin's user.role field is re-exposed via module augmentation
  // below, so consumers (e.g. transcript-web me.ts) still see session.user.role.
  return betterAuth({
    database,
    baseURL,
    secret,
    ...(trustedOrigins ? { trustedOrigins } : {}),
    rateLimit: resolveRateLimit(rateLimit),
    emailAndPassword: {
      enabled: true,
      requireEmailVerification: true,
      ...(kratosPasswords
        ? {
            password: {
              // Sign-in refuses before reaching the verifier when the account
              // carries no hash, so a sign-up must still write one. It is a
              // constant that validates nothing, never a hash of the password.
              hash: async () => KRATOS_SENTINEL_HASH,
              verify: kratosVerifier(kratosPasswords),
            },
          }
        : {}),
    },
    // Never auto-merge a social identity into an existing account by matching
    // email. Better Auth links by default (email-verified providers are trusted),
    // so signing in with Google/GitHub on an email already registered would fold
    // that identity into the existing account. We keep each sign-in method its
    // own account: a social login on a taken email is refused, not linked.
    // The suite's own identity provider is the one exception: it verifies
    // emails itself, and the same person must land on the same account
    // whether they signed in here before the SSO existed or not.
    account: {
      accountLinking: sso
        ? { enabled: true, trustedProviders: [ssoProviderId] }
        : { enabled: false },
    },
    ...(kratosPasswords
      ? {
          user: {
            additionalFields: {
              identityId: {
                type: "string",
                required: false,
                input: false,
              },
            },
          },
        }
      : {}),
    // Naming happens here rather than on /sign-up/email so that every way in
    // is covered: a magic link that signs up bypasses the endpoint entirely
    // and calls createUser straight, with `name: name || ""`.
    databaseHooks: {
      ...databaseHooks,
      account: withSsoRoleSync(databaseHooks?.account, sso, ssoProviderId),
      user: {
        ...databaseHooks?.user,
        create: {
          ...databaseHooks?.user?.create,
          after: withIdentityProvisioning(
            databaseHooks?.user?.create?.after,
            kratosPasswords,
          ),
          before: async (user: Record<string, unknown>, ctx: unknown) => {
            const named = withSignUpName(user)
            const appHook = databaseHooks?.user?.create?.before
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const applied = await appHook?.(named as any, ctx as any)
            if (applied === false) return false
            if (applied && typeof applied === "object" && "data" in applied) {
              return { data: withSignUpName(applied.data) }
            }
            return { data: named }
          },
        },
      },
    },
    hooks: {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      before: async (ctx: any) => {
        if (kratosPasswords && ctx.path === "/sign-in/email") {
          const body = ctx.body as { email?: string; password?: string } | undefined
          if (body?.email && body?.password) {
            rememberSignInIdentifier(body.password, body.email)
          }
        }
        if (!betaMode) return
        if (ctx.path !== "/sign-up/email") return
        const body = ctx.body as { email?: string; inviteToken?: string } | undefined
        const email = body?.email
        const inviteToken = body?.inviteToken
        if (email && inviteToken && isInvited) {
          const ok = await isInvited(email, inviteToken)
          if (ok) return
        }
        throw new APIError("FORBIDDEN", {
          message: "Registration is invite-only during the private beta.",
        })
      },
    },
    plugins: [
      emailOTP({
        async sendVerificationOTP({ email, otp, type }) {
          const subject = subjects[type]
            ? `${subjects[type]} - ${appName}`
            : `Your ${appName} code`
          const html = renderEmail(otp, type as PlatformAuthMailerType)

          if (mailer) {
            await mailer({
              to: email,
              subject,
              html,
              type: type as PlatformAuthMailerType,
              otp,
            })
            return
          }

          console.warn(
            `[EMAIL] No mailer configured — logging OTP to stdout for ${email} (${type}): ${otp}`,
          )
        },
        otpLength: 6,
        expiresIn: 300,
        overrideDefaultEmailVerification: true,
      }),
      ...(magicLinkConfig
        ? [
            magicLink({
              expiresIn: magicLinkConfig.expiresIn ?? 300,
              // A magic link that signs up walks past both gates the platform
              // puts on the front door: requireEmailVerification, and the
              // invite-only hook, which only guards /sign-up/email.
              disableSignUp: !magicLinkConfig.allowSignUp,
              async sendMagicLink({ email, url }) {
                const subject =
                  magicLinkConfig.subject ?? `Your sign-in link - ${appName}`
                const html = (
                  magicLinkConfig.render ?? defaultRenderMagicLinkEmail
                )(url, email)

                if (mailer) {
                  await mailer({
                    to: email,
                    subject,
                    html,
                    type: "magic-link",
                    url,
                  })
                  return
                }

                console.warn(
                  `[EMAIL] No mailer configured — logging magic link to stdout for ${email}: ${url}`,
                )
              },
            }),
          ]
        : []),
      admin(),
      ...(sso
        ? [
            genericOAuth({
              config: [
                {
                  providerId: ssoProviderId,
                  ...ssoEndpoints(sso.issuer),
                  clientId: sso.clientId,
                  clientSecret: sso.clientSecret,
                  scopes: ["openid", "email", "profile", "offline_access"],
                  pkce: true,
                  overrideUserInfo: true,
                  disableSignUp: sso.allowSignUp === false,
                  mapProfileToUser: (profile) =>
                    mapSsoProfile(profile as SsoProfile, sso.adminRole),
                },
              ],
            }),
          ]
        : []),
      ...(twoFactorConfig?.enabled
        ? [
            twoFactor({
              issuer: twoFactorConfig.issuer ?? appName,
              skipVerificationOnEnable:
                twoFactorConfig.skipVerificationOnEnable ?? false,
            }),
          ]
        : []),
      ...plugins, // app-specific plugins (e.g. tanstackStartCookies)
    ],
    socialProviders: {
      // Spread as given rather than rebuilt field by field: anything Better
      // Auth accepts belongs to the app, and a config silently dropped on the
      // way through is how an app ends up writing a plugin to put it back.
      //
      // The two defaults below are the platform's, not Google's: without
      // accessType 'offline' Google never mints a refresh token, and without
      // 'consent' it stops minting one for an account that already consented.
      // A NULL refreshToken means deleting an account can revoke the access
      // token but cannot remove the app from myaccount.google.com/permissions,
      // so the grant outlives the account it belonged to.
      ...(google ? { google: withGoogleDefaults(google) } : {}),
      ...(github ? { github } : {}),
    },
  }) as unknown as Auth<BetterAuthOptions>
}

type UserHooks = NonNullable<NonNullable<BetterAuthOptions["databaseHooks"]>["user"]>
type UserAfterHook = NonNullable<NonNullable<UserHooks["create"]>["after"]>

/**
 * Gives the new local user an identity at the provider and stores its id.
 *
 * This runs after the insert commits, so it cannot be atomic with the
 * sign-up: a provider that is down leaves `identityId` null and the person
 * registered all the same. The endpoint is idempotent on the address, so the
 * repair re-sends without risking a second identity.
 */
function withIdentityProvisioning(
  own: UserAfterHook | undefined,
  config: PlatformKratosPasswordConfig | undefined,
): UserAfterHook | undefined {
  if (!config) return own
  return async (user, ctx) => {
    await own?.(user, ctx)
    const record = user as { id?: string; email?: string; name?: string }
    if (!record.id || !record.email) return

    const outcome = await provisionIdentity(config, {
      email: record.email,
      name: record.name,
    })

    if (outcome.status === "provisioned" && ctx) {
      await ctx.context.internalAdapter.updateUser(record.id, {
        identityId: outcome.identityId,
      })
      return
    }

    await config.onProvisioningDeferred?.({ userId: record.id, email: record.email })
  }
}

type AccountHooks = NonNullable<NonNullable<BetterAuthOptions["databaseHooks"]>["account"]>
type AccountAfterHook = NonNullable<NonNullable<AccountHooks["create"]>["after"]>

// The admin plugin declares `role` as not settable from input, so the role
// mapProfileToUser returns is dropped when the OAuth path creates the user.
// The account row, created then refreshed on every sign-in, carries the ID
// token: its roles claim is what sets the local role, each time.
function withSsoRoleSync(
  hooks: AccountHooks | undefined,
  sso: PlatformSsoConfig | undefined,
  providerId: string,
): AccountHooks | undefined {
  if (!sso) return hooks
  const sync: AccountAfterHook = async (account, ctx) => {
    if (account.providerId !== providerId || !ctx) return
    const role = roleFromIdToken(account.idToken, sso.adminRole)
    if (!role) return
    await ctx.context.internalAdapter.updateUser(account.userId, { role })
  }
  const chain =
    (own: AccountAfterHook | undefined): AccountAfterHook =>
    async (account, ctx) => {
      await own?.(account, ctx)
      await sync(account, ctx)
    }
  return {
    ...hooks,
    create: { ...hooks?.create, after: chain(hooks?.create?.after) },
    update: { ...hooks?.update, after: chain(hooks?.update?.after) },
  }
}

/**
 * Better Auth's verifier is handed the stored hash and the submitted password,
 * never the address, and the same verifier serves sign-in, password change and
 * account deletion. The address of the sign-in being processed is carried here
 * by the route hook, keyed by the submitted password so two concurrent
 * sign-ins cannot read each other's.
 */
const pendingIdentifiers = new Map<string, string>()

export function rememberSignInIdentifier(password: string, email: string): void {
  pendingIdentifiers.set(password, email.trim().toLowerCase())
}

function takeSignInIdentifier(password: string): string | undefined {
  const email = pendingIdentifiers.get(password)
  pendingIdentifiers.delete(password)
  return email
}

export class KratosSignInError extends APIError {
  constructor(status: "UNAUTHORIZED" | "FORBIDDEN" | "SERVICE_UNAVAILABLE", code: string, message: string) {
    super(status, { code, message })
  }
}

function refusalFor(outcome: KratosOutcome): KratosSignInError | undefined {
  switch (outcome.status) {
    case "no_credential":
      return new KratosSignInError(
        "FORBIDDEN",
        "IDENTITY_HAS_NO_PASSWORD",
        "This account has no password yet at the identity provider. Use the password recovery to set one.",
      )
    case "second_factor_required":
      return new KratosSignInError(
        "FORBIDDEN",
        "SECOND_FACTOR_REQUIRED",
        "A second factor is required to sign in.",
      )
    case "account_disabled":
      return new KratosSignInError(
        "FORBIDDEN",
        "ACCOUNT_DISABLED",
        "This account is deactivated.",
      )
    case "unavailable":
      return new KratosSignInError(
        "SERVICE_UNAVAILABLE",
        "IDENTITY_PROVIDER_UNAVAILABLE",
        "The identity service is unavailable. Your password has not been refused — try again shortly.",
      )
    default:
      return undefined
  }
}

/**
 * Fails closed: anything other than an explicit success refuses the sign-in,
 * and only an explicit refusal by Kratos reads as a wrong password. An outage
 * answers 503, so nobody is told their password is wrong and rotates a
 * password that was right.
 */
function kratosVerifier(config: PlatformKratosPasswordConfig) {
  return async ({ password }: { hash: string; password: string }): Promise<boolean> => {
    const email = takeSignInIdentifier(password)
    if (!email) return false
    const outcome = await verifyAgainstKratos(config.publicUrl, { email, password })
    if (outcome.status === "valid") return true
    const refusal = refusalFor(outcome)
    if (refusal) throw refusal
    return false
  }
}

export type PlatformAuth = ReturnType<typeof createPlatformAuth>

// Re-export the session contract from /server so consumers that import the
// auth factory can type api.getSession() without a second import path.
export type {
  PlatformUser,
  PlatformSession,
  PlatformSessionData,
  PlatformRateLimitConfig,
  PlatformRateLimitRule,
  PlatformTwoFactorConfig,
} from "./types"

// Invitation claiming runs on the auth callback, where the session is
// established — the one place every sign-up flow passes through.
export {
  claimInvitation,
  completesSignup,
  holdInviteTokenCookie,
  invitationOutcomeCookie,
  inviteTokenFrom,
  isInvitationFailure,
  pinInviteToken,
  releaseInviteTokenCookie,
} from "./invitation"
export type { ClaimOutcome, ClaimInvitationOptions } from "./invitation"

export { mapSsoProfile } from "./sso-profile"
export type { SsoProfile, SsoMappedUser } from "./sso-profile"

export { KRATOS_SENTINEL_HASH, isKratosSentinel } from "./kratos-credentials"
export type { KratosOutcome } from "./kratos-credentials"

// The repair path an app schedules for the sign-ups whose provisioning could
// not reach the provider.
export { provisionIdentity } from "./identity-provisioning"
export type {
  IdentityProvisioningConfig,
  ProvisionOutcome,
} from "./identity-provisioning"

export { bootstrapFirstAdmin } from "./bootstrap-admin"
export type {
  BootstrapAdminPool,
  BootstrapAdminClient,
  BootstrapFirstAdminInput,
  BootstrapFirstAdminResult,
} from "./bootstrap-admin"
