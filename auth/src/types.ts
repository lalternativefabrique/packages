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

export interface VerifyEmailFormProps extends AuthThemeProps, AuthNavProps {
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
  /** The urbangate client */
  authClient: AuthClientSurface
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
  emailNotVerified?: string
  accountNotLinked?: string
  socialCancelled?: string
  socialFailed?: string
}

export interface RegisterFormLabels {
  title?: string
  subtitle?: string
  namePlaceholder?: string
  optional?: string
  emailPlaceholder?: string
  emailLocked?: string
  passwordPlaceholder?: string
  passwordHint?: string
  confirmPlaceholder?: string
  passwordMismatch?: string
  submit?: string
  submitPending?: string
  haveAccount?: string
  login?: string
  emailRequired?: string
  passwordTooShort?: string
  signUpFailed?: string
  accountNotLinked?: string
  alreadyRegistered?: string
  socialCancelled?: string
  socialFailed?: string
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

/**
 * Per-screen colour overrides.
 *
 * The components paint themselves from the host's theme tokens (`--primary`,
 * `--card`, `--input`, `--ring`), so an app that sets those gets a coherent
 * surface for free and needs none of this. These are for the case where one
 * screen has to differ from the theme — a branded submit on an otherwise
 * neutral admin, say. Each replaces the colour utilities it names, so pass the
 * whole look rather than an addition to it.
 */
/**
 * The client surface these screens call, declared structurally: the urbangate
 * clients provide it, and a client missing a call is a compile error instead
 * of a runtime one.
 */
export interface AuthClientResult {
  error?: { message?: string; code?: string; status?: number } | null
}

export interface AuthClientDataResult<T> extends AuthClientResult {
  data?: T | null
}

export interface AuthClientSurface {
  signIn: {
    email(input: { email: string; password: string }): Promise<AuthClientResult>
    social(input: {
      provider: "google" | "github"
      callbackURL?: string
      errorCallbackURL?: string
    }): Promise<AuthClientResult>
  }
  signUp: {
    email(input: {
      /** Omitted when the sign-up form's optional name field is left blank */
      name?: string
      email: string
      password: string
      callbackURL?: string
    }): Promise<AuthClientResult>
  }
  emailOtp: {
    verifyEmail(input: { email: string; otp: string }): Promise<AuthClientResult>
    sendVerificationOtp(input: {
      email: string
      type: "email-verification" | "forget-password" | "sign-in"
    }): Promise<AuthClientResult>
    resetPassword(input: {
      email: string
      otp: string
      password: string
    }): Promise<AuthClientResult>
  }
  /** The second step of a password reset for an identity holding a second factor. */
  secondFactor?: {
    verify(input: { code: string; password: string }): Promise<AuthClientResult>
  }
}

export interface AuthThemeProps {
  /** Replaces the submit button's `bg-primary text-primary-foreground` */
  submitClassName?: string
  /** Replaces a field's border and background utilities */
  fieldClassName?: string
}

/**
 * Router link used for the navigation between auth screens.
 *
 * Every consuming app routes client-side, where a bare anchor triggers a full
 * document load: the app boots again, and an invitation held in the URL is
 * dropped on the way. Apps without a router pass nothing and get an anchor.
 */
export type LinkComponent = React.ComponentType<{
  to: string
  className?: string
  children: React.ReactNode
}>

export interface AuthNavProps {
  /** Client-side link component. Defaults to a plain anchor. */
  linkComponent?: LinkComponent
}

export interface AuthInviteProps {
  /**
   * Invitation token carried by the URL. The screen surfaces it and keeps it on
   * the links to its sibling screens and on the OAuth callback; redeeming it is
   * the auth handler's job — a social sign-up leaves the browser, so no
   * component is mounted to do it.
   */
  invite?: string
}

export interface LoginFormProps extends AuthThemeProps, AuthNavProps, AuthInviteProps {
  /** Callback once the session cookie is set and the core token minted */
  onSuccess?: () => void
  /**
   * Called with the typed address when the credentials are right but the
   * address was never confirmed, so the sign-in is refused without a session.
   * Route to the OTP screen, as `RegisterForm.onSuccess` does. Left unset, the
   * refusal is rendered as an error like any other, which reads as a wrong
   * password.
   */
  onEmailNotVerified?: (email: string) => void
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
   * Where the browser comes back after a social sign-in. Social buttons are
   * only rendered when at least one provider is passed.
   */
  socialCallbackUrl?: string
  /**
   * Where a social sign-in that did NOT work sends the browser, with `?error=`
   * on it. Defaults to the page this form is on, the one that reads the code
   * with `initialOAuthError` and renders it.
   */
  errorCallbackUrl?: string
  /** Social providers to offer, in display order */
  socialProviders?: Array<"google" | "github">
  /**
   * Endpoint renewing the core's token once the session is open. Called after
   * a successful password sign-in; pass null to skip when the app has no core.
   */
  coreTokenUrl?: string | null
  /** Copy overrides; anything omitted keeps the French default */
  labels?: LoginFormLabels
  /** The urbangate client */
  authClient: AuthClientSurface
}

export interface EmailCodeSignInFormLabels {
  title?: string
  subtitle?: string
  emailPlaceholder?: string
  codePlaceholder?: string
  send?: string
  submit?: string
  pending?: string
  sendFailed?: string
  invalidCode?: string
  unavailable?: string
  changeEmail?: string
  usePassword?: string
}

export interface EmailCodeSignInClientSurface {
  emailOtp: {
    sendVerificationOtp(input: {
      email: string
      type: "sign-in"
    }): Promise<AuthClientResult>
  }
  signIn: {
    emailOtp(input: { email: string; otp: string }): Promise<AuthClientResult>
  }
}

export interface EmailCodeSignInFormProps extends AuthThemeProps, AuthNavProps {
  onSuccess?: () => void
  error?: string
  /** Link back to the password sign-in */
  passwordSignInUrl?: string
  /** Called once signed in to renew the core's token; null skips it */
  coreTokenUrl?: string | null
  labels?: EmailCodeSignInFormLabels
  authClient: EmailCodeSignInClientSurface
}

export interface RegisterFormProps extends AuthThemeProps, AuthNavProps, AuthInviteProps {
  /**
   * Address the invitation was issued to. Given, it fills the email field and
   * fixes it: an invitation grants its tier to one address, so signing up with
   * another would drop the grant with nothing said about it.
   */
  lockedEmail?: string
  /** Callback on successful sign-up, receives the email to verify */
  onSuccess?: (email: string) => void
  /** Error raised outside the form, rendered in the same banner */
  error?: string
  /** Link to the login page */
  loginUrl?: string
  /**
   * Whether to ask for a name. On by default.
   *
   * Turn it off where the name is collected later — a checkout that needs it
   * for the invoice, say. Sign-up then sends none, and the server decides what
   * an account without one is called.
   */
  collectName?: boolean
  /** Rendered under the submit button — typically terms and privacy links */
  legal?: React.ReactNode
  /** Where the browser comes back after a social sign-up */
  socialCallbackUrl?: string
  /**
   * Where a social sign-up that did NOT work sends the browser, with `?error=`
   * on it. Defaults to the page this form is on, the one that reads the code
   * with `initialOAuthError` and renders it.
   */
  errorCallbackUrl?: string
  /** Social providers to offer, in display order */
  socialProviders?: Array<"google" | "github">
  /** Copy overrides; anything omitted keeps the French default */
  labels?: RegisterFormLabels
  /** The urbangate client */
  authClient: AuthClientSurface
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

export interface ForgotPasswordFormProps extends AuthThemeProps, AuthNavProps {
  /** Callback on successful OTP send, receives the email */
  onSuccess?: (email: string) => void
  /** Link to login page */
  loginUrl?: string
  /** Copy overrides; anything omitted keeps the French default */
  labels?: ForgotPasswordFormLabels
  /** The urbangate client */
  authClient: AuthClientSurface
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
  codeSpent?: string
  secondFactorTitle?: string
  secondFactorPlaceholder?: string
  secondFactorHint?: string
  secondFactorSubmit?: string
  secondFactorInvalid?: string
}

export interface ResetPasswordFormProps extends AuthThemeProps, AuthNavProps {
  /** Email address to reset password for */
  email: string
  /** Callback on successful password reset */
  onSuccess?: () => void
  /** Link to login page */
  loginUrl?: string
  /** Copy overrides; anything omitted keeps the French default */
  labels?: ResetPasswordFormLabels
  /** The urbangate client */
  authClient: AuthClientSurface
}

export interface AuthLayoutProps extends AuthHeadingProps {
  /** The app's mark, rendered above the card */
  logo?: React.ReactNode
  /**
   * Illustration filling the LEFT half of the card from md up, dropped below
   * it. Any node: an `<img className="h-full w-full object-cover">`, a
   * gradient, a testimonial. Purely decorative — it is hidden from assistive
   * technology, so it must not carry anything the form does not say. Omit it
   * and the card stays a single column.
   */
  panel?: React.ReactNode
  /**
   * Heading above the card. Each form exports its default copy as
   * `<Form>.defaults`, so a screen can pass it through or replace it.
   */
  title?: string
  subtitle?: string
  /** The form, rendered inside the card */
  children: React.ReactNode
  /** Footer content below the card (e.g. legal links) */
  footer?: React.ReactNode
}
