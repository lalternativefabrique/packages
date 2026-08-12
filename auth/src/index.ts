// Types
export type {
  PlatformAuthConfig,
  PlatformAuthClientConfig,
  PlatformAuthMailer,
  PlatformAuthMailerArgs,
  PlatformAuthMailerType,
  PlatformUser,
  PlatformSession,
  PlatformSessionData,
  LoginFormProps,
  LoginFormLabels,
  RegisterFormProps,
  RegisterFormLabels,
  VerifyEmailFormProps,
  ForgotPasswordFormProps,
  MagicLinkFormProps,
  MagicLinkFormLabels,
  MagicLinkClientSurface,
  MagicLinkConfig,
  ResetPasswordFormProps,
  AuthLayoutProps,
  InvitationNoticeProps,
  InvitationFailure,
  AuthClientSurface,
  AuthClientResult,
  AuthThemeProps,
  AuthNavProps,
  AuthInviteProps,
  LinkComponent,
} from "./types"

// Hooks
export { useSession, useLogout } from "./hooks/use-session"

// Components
export { AuthField } from "./components/auth-field"
export type { AuthFieldProps } from "./components/auth-field"
export { AuthSubmit } from "./components/auth-submit"
export { LoginForm } from "./components/login-form"
export { RegisterForm } from "./components/register-form"
export { SocialButtons } from "./components/social-buttons"
export { VerifyEmailForm } from "./components/verify-email-form"
export { ForgotPasswordForm } from "./components/forgot-password-form"
export { MagicLinkForm } from "./components/magic-link-form"
export { ResetPasswordForm } from "./components/reset-password-form"
export { AuthLayout } from "./components/auth-layout"
export { InvitationNotice } from "./components/invitation-notice"

export { normalizeInviteToken, withInviteToken } from "./invite-token"

// A route guarding on the same refusal outside the form needs the predicate.
export { isEmailNotVerified } from "./email-not-verified"

// OAuth failures reach the page through the address bar, so a route reading one
// before render needs these outside the components.
export {
  clearOAuthError,
  initialOAuthError,
  oauthErrorCallback,
  oauthErrorMessage,
} from "./oauth-error"
export type { OAuthErrorLabels } from "./oauth-error"

// A followed magic link fails the same way an OAuth round-trip does: by coming
// back with `?error=`, with no component mounted to have caught it.
export {
  initialMagicLinkError,
  isMagicLinkError,
  magicLinkErrorCallback,
  magicLinkErrorMessage,
} from "./magic-link-error"
export type { MagicLinkErrorLabels } from "./magic-link-error"

export { AuthLink } from "./components/auth-link"

// Invitations. claimInvitation itself is server-only and lives in /server; what
// is exported here is what a page rendering InvitationNotice needs.
export { isInvitationFailure } from "./invitation"
export type { ClaimOutcome } from "./invitation"
