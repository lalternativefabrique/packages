import type { BetterAuthOptions } from "better-auth"

/**
 * Session user shape exposed by the platform auth instance.
 *
 * The instance returned by {@link createPlatformAuth} is widened to the base
 * `Auth` type so the published `.d.ts` stays portable (inferring the full
 * plugin-augmented type triggers TS2742). That widening hides the fields the
 * email-otp/admin plugins add at runtime — notably `role` from `admin()`.
 *
 * This is the hand-maintained contract for what `api.getSession()` actually
 * returns. Consumers cast the session to {@link PlatformSession} to read these
 * fields with types. Keep it in sync with the enabled plugins.
 */
export interface PlatformUser {
  id: string
  email: string
  emailVerified: boolean
  name: string
  image?: string | null
  createdAt: Date
  updatedAt: Date
  /** From the admin() plugin. Absent until a role is assigned. */
  role?: string | null
  /** From the admin() plugin. */
  banned?: boolean | null
}

export interface PlatformSessionData {
  id: string
  userId: string
  expiresAt: Date
  token: string
  createdAt: Date
  updatedAt: Date
  ipAddress?: string | null
  userAgent?: string | null
}

/** Return shape of `auth.api.getSession()` for platform apps. */
export interface PlatformSession {
  user: PlatformUser
  session: PlatformSessionData
}

export type PlatformAuthMailerType =
  | "email-verification"
  | "forget-password"
  | "sign-in"
  | "change-email"

export interface PlatformAuthMailerArgs {
  /** Recipient address */
  to: string
  /** Pre-rendered subject line */
  subject: string
  /** Pre-rendered HTML body */
  html: string
  /** Better Auth verification kind */
  type: PlatformAuthMailerType
  /** The OTP value, in case the consumer wants to render its own template */
  otp: string
}

export type PlatformAuthMailer = (args: PlatformAuthMailerArgs) => Promise<void>

export interface PlatformAuthConfig {
  /** PostgreSQL connection pool or connection string */
  database: BetterAuthOptions["database"]
  /** Base URL for Better Auth callbacks (e.g. http://localhost:3001) */
  baseURL: string
  /** Secret for signing sessions */
  secret: string
  /** Application name (used in emails) */
  appName: string
  /**
   * Transactional mailer. Receives the fully-rendered subject and HTML body
   * and is responsible for pushing the message onto the wire (e.g. via the
   * @digstack/spore-sdk, SES, postfix, …). When omitted, OTPs are logged to
   * stdout — useful in dev/test, useless in production.
   */
  mailer?: PlatformAuthMailer
  /** Google OAuth config (omit to disable) */
  google?: {
    clientId: string
    clientSecret: string
  }
  /** GitHub OAuth config (omit to disable) */
  github?: {
    clientId: string
    clientSecret: string
  }
  /**
   * Override the OTP email subject line per verification type. Merged over
   * the platform defaults — provide only the keys you want to change. The
   * resulting subject is suffixed with ` - ${appName}` like the defaults.
   */
  emailSubjects?: Partial<Record<PlatformAuthMailerType, string>>
  /**
   * Override the OTP email HTML renderer. Receives the OTP code and the
   * verification type, returns the HTML body. When omitted, the platform's
   * default branded template is used.
   */
  renderOtpEmail?: (otp: string, type: PlatformAuthMailerType) => string
  /**
   * Better Auth database hooks, passed through unchanged.
   *
   * `user.create.after` is the seam an app uses to react to a signup — record
   * the invite token it carried, queue the work that puts the account on a
   * plan. It runs AFTER the insert commits (Better Auth queues it as an
   * after-transaction hook), so it cannot be made atomic with the user row:
   * a crash between the two leaves an account nothing reacted to. Anything
   * durable therefore needs its own repair path.
   */
  databaseHooks?: BetterAuthOptions["databaseHooks"]
  /** Additional Better Auth plugins to append */
  plugins?: BetterAuthOptions["plugins"]
  /** Enable private beta mode (blocks public registration) */
  betaMode?: boolean
  /** Check if an email+token pair has been invited (required when betaMode is true) */
  isInvited?: (email: string, inviteToken: string) => Promise<boolean>
}

export interface PlatformAuthClientConfig {
  /** Base URL override (defaults to window.location.origin in browser) */
  baseURL?: string
  /** Additional client plugins */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  plugins?: any[]
}

export interface VerifyEmailFormLabels {
  title?: string
  subtitle?: string
  subtitleNoEmail?: string
  codePlaceholder?: string
  submit?: string
  submitPending?: string
  resend?: string
  resendPending?: string
  resent?: string
  alreadyVerified?: string
  login?: string
  codeRequired?: string
  invalidCode?: string
  resendFailed?: string
  missingEmail?: string
}

export interface VerifyEmailFormProps extends AuthHeadingProps {
  /** Email to verify */
  email: string
  /** Callback on successful verification */
  onSuccess?: () => void
  /** URL to navigate to on success */
  successUrl?: string
  /** Link to the login page */
  loginUrl?: string
  /** Copy overrides; anything omitted keeps the French default */
  labels?: VerifyEmailFormLabels
  /** Auth client instance */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  authClient: any
}

/** Why an invitation link did not work, as far as the invitee needs to know. */
export type InvitationFailure = "expired" | "claimed" | "unknown"

export interface InvitationNoticeProps {
  reason?: InvitationFailure
  /** Defaults to contact@ the apex domain the app is served from. */
  supportEmail?: string
  title?: string
  /** Rendered under the contact line — typically a link back to the site. */
  action?: React.ReactNode
}

/**
 * Copy overrides for the sign-in screen. Every key is optional; what is not
 * given falls back to the French defaults, matching InvitationNotice and the
 * apps consuming this package. Pass a full set to render another language.
 */
export interface LoginFormLabels {
  title?: string
  subtitle?: string
  emailPlaceholder?: string
  passwordPlaceholder?: string
  forgotPassword?: string
  submit?: string
  submitPending?: string
  noAccount?: string
  register?: string
  emailRequired?: string
  passwordRequired?: string
  invalidCredentials?: string
  accountNotLinked?: string
  socialCancelled?: string
  socialFailed?: string
}

export interface RegisterFormLabels {
  title?: string
  subtitle?: string
  namePlaceholder?: string
  emailPlaceholder?: string
  passwordPlaceholder?: string
  passwordHint?: string
  submit?: string
  submitPending?: string
  haveAccount?: string
  login?: string
  nameRequired?: string
  emailRequired?: string
  passwordTooShort?: string
  signUpFailed?: string
}

/**
 * Styling of the form's own heading. The package fixes the structure and a
 * neutral default; the app fixes the type. Passing a class replaces the
 * default size and weight, so pass the whole look, not an addition to it.
 */
export interface AuthHeadingProps {
  /** Replaces the default `text-2xl font-semibold tracking-tight` */
  titleClassName?: string
}

export interface LoginFormProps extends AuthHeadingProps {
  /** Callback once the session cookie is set and the core token minted */
  onSuccess?: () => void
  /**
   * Error raised outside the form — a failed OAuth round-trip coming back as
   * `?error=`, a guard bouncing an unauthenticated visitor. Rendered in the
   * same banner as the form's own errors, and superseded by them on submit.
   */
  error?: string
  /** Link to the registration page */
  registerUrl?: string
  /** Link to the password recovery page */
  forgotPasswordUrl?: string
  /**
   * Where Better Auth sends the browser back after a social sign-in. Social
   * buttons are only rendered when at least one provider is passed.
   */
  socialCallbackUrl?: string
  /** Social providers to offer, in display order */
  socialProviders?: Array<"google" | "github">
  /**
   * Endpoint trading the fresh better-auth session for the short-lived EdDSA
   * token the Go core verifies. Called after a successful password sign-in;
   * pass null to skip when the app has no core.
   */
  coreTokenUrl?: string | null
  /** Copy overrides; anything omitted keeps the French default */
  labels?: LoginFormLabels
  /** Auth client instance */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  authClient: any
}

export interface RegisterFormProps extends AuthHeadingProps {
  /** Callback on successful sign-up, receives the email to verify */
  onSuccess?: (email: string) => void
  /** Error raised outside the form, rendered in the same banner */
  error?: string
  /** Link to the login page */
  loginUrl?: string
  /** Rendered under the submit button — typically terms and privacy links */
  legal?: React.ReactNode
  /** Where Better Auth sends the browser back after a social sign-up */
  socialCallbackUrl?: string
  /** Social providers to offer, in display order */
  socialProviders?: Array<"google" | "github">
  /** Copy overrides; anything omitted keeps the French default */
  labels?: RegisterFormLabels
  /** Auth client instance */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  authClient: any
}

export interface ForgotPasswordFormLabels {
  title?: string
  subtitle?: string
  emailPlaceholder?: string
  submit?: string
  submitPending?: string
  rememberPassword?: string
  login?: string
  emailRequired?: string
  sendFailed?: string
}

export interface ForgotPasswordFormProps extends AuthHeadingProps {
  /** Callback on successful OTP send, receives the email */
  onSuccess?: (email: string) => void
  /** Link to login page */
  loginUrl?: string
  /** Copy overrides; anything omitted keeps the French default */
  labels?: ForgotPasswordFormLabels
  /** Auth client instance */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  authClient: any
}

export interface ResetPasswordFormLabels {
  title?: string
  subtitle?: string
  codePlaceholder?: string
  passwordPlaceholder?: string
  passwordHint?: string
  confirmPlaceholder?: string
  submit?: string
  submitPending?: string
  resend?: string
  resendPending?: string
  resent?: string
  rememberPassword?: string
  login?: string
  codeRequired?: string
  passwordTooShort?: string
  passwordMismatch?: string
  resetFailed?: string
  resendFailed?: string
}

export interface ResetPasswordFormProps extends AuthHeadingProps {
  /** Email address to reset password for */
  email: string
  /** Callback on successful password reset */
  onSuccess?: () => void
  /** Link to login page */
  loginUrl?: string
  /** Copy overrides; anything omitted keeps the French default */
  labels?: ResetPasswordFormLabels
  /** Auth client instance */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  authClient: any
}

export interface AuthLayoutProps {
  /** The app's mark, rendered above the form inside the card */
  logo?: React.ReactNode
  /**
   * Illustration filling the right half of the card from md up, dropped below
   * it. Any node: an `<img className="h-full w-full object-cover">`, a
   * gradient, a testimonial. Purely decorative — it is hidden from assistive
   * technology, so it must not carry anything the form does not say.
   */
  panel?: React.ReactNode
  /** The form. It renders its own title. */
  children: React.ReactNode
  /** Footer content below the card (e.g. legal links) */
  footer?: React.ReactNode
}
