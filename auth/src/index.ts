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
  ResetPasswordFormProps,
  AuthLayoutProps,
  InvitationNoticeProps,
  InvitationFailure,
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
export { ResetPasswordForm } from "./components/reset-password-form"
export { AuthLayout } from "./components/auth-layout"
export { InvitationNotice } from "./components/invitation-notice"

// Invitations. claimInvitation itself is server-only and lives in /server; what
// is exported here is what a page rendering InvitationNotice needs.
export { isInvitationFailure } from "./invitation"
export type { ClaimOutcome } from "./invitation"
