import { betterAuth, APIError, type Auth, type BetterAuthOptions } from "better-auth"
import { emailOTP, admin } from "better-auth/plugins"
import type { PlatformAuthConfig, PlatformAuthMailerType } from "./types"

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
    betaMode = false,
    isInvited,
    emailSubjects,
    renderOtpEmail,
  } = config

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
    emailAndPassword: {
      enabled: true,
      requireEmailVerification: true,
    },
    // Never auto-merge a social identity into an existing account by matching
    // email. Better Auth links by default (email-verified providers are trusted),
    // so signing in with Google/GitHub on an email already registered would fold
    // that identity into the existing account. We keep each sign-in method its
    // own account: a social login on a taken email is refused, not linked.
    account: {
      accountLinking: {
        enabled: false,
      },
    },
    hooks: {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      before: async (ctx: any) => {
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
      admin(),
      ...plugins, // app-specific plugins (e.g. tanstackStartCookies)
    ],
    socialProviders: {
      ...(google
        ? {
            google: {
              clientId: google.clientId,
              clientSecret: google.clientSecret,
            },
          }
        : {}),
      ...(github
        ? {
            github: {
              clientId: github.clientId,
              clientSecret: github.clientSecret,
            },
          }
        : {}),
    },
  }) as unknown as Auth<BetterAuthOptions>
}

export type PlatformAuth = ReturnType<typeof createPlatformAuth>

// Re-export the session contract from /server so consumers that import the
// auth factory can type api.getSession() without a second import path.
export type {
  PlatformUser,
  PlatformSession,
  PlatformSessionData,
} from "./types"

// Invitation claiming runs on the auth callback, where the session is
// established — the one place every sign-up flow passes through.
export {
  claimInvitation,
  inviteTokenFrom,
  isInvitationFailure,
} from "./invitation"
export type { ClaimOutcome, ClaimInvitationOptions } from "./invitation"
