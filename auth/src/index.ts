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
export { VerifyEmailForm } from "./components/verify-email-form"
export { ForgotPasswordForm } from "./components/forgot-password-form"
export { ResetPasswordForm } from "./components/reset-password-form"
export { AuthLayout } from "./components/auth-layout"
export { InvitationNotice } from "./components/invitation-notice"

// Invitations. claimInvitation itself is server-only and lives in /server; what
// is exported here is what a page rendering InvitationNotice needs.
export { isInvitationFailure } from "./invitation"
export type { ClaimOutcome } from "./invitation"
