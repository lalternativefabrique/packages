// Types
export type {
  LoginFormProps,
  LoginFormLabels,
  EmailCodeSignInFormProps,
  EmailCodeSignInFormLabels,
  EmailCodeSignInClientSurface,
  RegisterFormProps,
  RegisterFormLabels,
  VerifyEmailFormProps,
  ForgotPasswordFormProps,
  ResetPasswordFormProps,
  AuthLayoutProps,
  InvitationLandingProps,
  InvitationLandingLabels,
  AuthClientSurface,
  AuthClientResult,
  AuthClientDataResult,
  AuthThemeProps,
  AuthNavProps,
  LinkComponent,
} from "./types"

// Components
export { AuthField } from "./components/auth-field"
export type { AuthFieldProps } from "./components/auth-field"
export { AuthSubmit } from "./components/auth-submit"
export { LoginForm } from "./components/login-form"
export { EmailCodeSignInForm } from "./components/email-code-sign-in-form"
export { RegisterForm } from "./components/register-form"
export { SocialButtons } from "./components/social-buttons"
export { VerifyEmailForm } from "./components/verify-email-form"
export { ForgotPasswordForm } from "./components/forgot-password-form"
export { ResetPasswordForm } from "./components/reset-password-form"
export { AuthLayout } from "./components/auth-layout"
export { InvitationLanding } from "./components/invitation-landing"

// A route guarding on the same refusal outside the form needs the predicate.
export { isEmailNotVerified } from "./email-not-verified"

// Sign-in refusals coming from the identity provider rather than from a wrong
// password: a page routing to recovery or reporting an outage needs to tell
// them apart.
export {
  isAccountDisabled,
  isIdentityProviderUnavailable,
  needsPasswordRecovery,
  needsSecondFactor,
} from "./kratos-sign-in-error"

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

export { earlyAccessOf, mapSsoProfile, rolesOf } from "./sso-profile"
export type { EarlyAccess, SsoProfile, SsoMappedUser } from "./sso-profile"

export { DeleteAccountSteps } from "./components/delete-account-steps"
export type {
  DeleteAccountStepsLabels,
  DeleteAccountStepsProps,
} from "./components/delete-account-steps"
export type {
  AccountDeletionReport,
  AccountDeletionStep,
  AccountDeletionStepId,
  AccountDeletionStepStatus,
} from "./account-deletion"

export { AccountSettings } from "./components/account-settings"
export type {
  AccountSettingsClient,
  AccountSettingsLabels,
  AccountSettingsProps,
  AccountSettingsSection,
} from "./components/account-settings"
export {
  MIN_PASSWORD_LENGTH,
  emailChangeProblem,
  passwordChangeProblem,
} from "./account-settings"
export { NakodaAnalytics } from "@lalternative/nakoda-sdk-react"
