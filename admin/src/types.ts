import type { ReactNode } from "react"

/**
 * A user row as shown in the admin table. Shaped after the Better Auth
 * `admin()` plugin's user (role/banned live there), kept as a plain interface
 * so the package does not depend on better-auth's types.
 */
export interface AdminUser {
  id: string
  email: string
  name?: string | null
  /** From the admin() plugin. Absent until a role is assigned. */
  role?: string | null
  /** From the admin() plugin. */
  banned?: boolean | null
  emailVerified: boolean
  createdAt: string | Date
}

/**
 * The user-facing profile returned by an app's `/api/me`. `roles` drives
 * {@link hasAdminFeatures}. Matches the shape apps already expose.
 */
export interface UserProfile {
  user_id: string
  email: string
  name: string
  avatar_url?: string
  roles: string[]
}

/**
 * Minimal capability interface the admin UI needs. Each app writes a one-line
 * adapter from its `authClient.admin.*` to this — so the package stays
 * decoupled from the auth client's (widened, role-hiding) type and from
 * better-auth itself.
 *
 * Methods return `unknown` on purpose: the UI only cares whether they resolve
 * or reject, not about the concrete Better Auth response envelope.
 */
export interface AdminUserApi {
  /** List users, most-recent first. `total` is optional (used for the count). */
  listUsers(query?: {
    limit?: number
    sortBy?: string
    sortDirection?: "asc" | "desc"
  }): Promise<{ users: AdminUser[]; total?: number }>
  /** Optional: delete via the plugin. Apps usually pass a server route instead. */
  removeUser?(userId: string): Promise<unknown>
  banUser(userId: string, reason?: string): Promise<unknown>
  unbanUser(userId: string): Promise<unknown>
  setRole(userId: string, role: string): Promise<unknown>
}

/**
 * The single Better Auth client method the login/setup flows call directly.
 * Apps pass their `authClient`; typed structurally so no better-auth import is
 * needed. `role` is read from the profile fetched separately (see
 * {@link AdminLoginFormProps.getProfile}).
 */
export interface AdminAuthClient {
  signIn: {
    email(args: {
      email: string
      password: string
    }): Promise<{ error?: { message?: string } | null }>
  }
  signOut(): Promise<unknown>
}

export interface AdminNavItem {
  /** Rendered label. */
  label: string
  /** The app supplies the actual link element (router-coupled). */
  render: (opts: { className: string; activeClassName: string }) => ReactNode
}

export interface AdminLayoutProps {
  /** Router-supplied nav links (Dashboard, Users, …). */
  nav?: ReactNode
  /** Router-supplied "back to app" link. */
  backToApp?: ReactNode
  /** Header title. Defaults to "Administration". */
  title?: string
  children: ReactNode
}

export interface AdminHomeProps {
  api: Pick<AdminUserApi, "listUsers">
  /** Router-supplied link to the users page (wraps the count card). */
  usersLink: (opts: { className: string; children: ReactNode }) => ReactNode
  /** Page heading. Defaults to "Tableau de bord". */
  title?: string
}

export interface UsersTableProps {
  api: AdminUserApi
  /**
   * Delete a user. Apps route this to their own server endpoint (cascade +
   * domain cleanup). Resolve on success; reject with a message on failure.
   * If omitted and `api.removeUser` exists, the plugin delete is used.
   */
  onDeleteUser?: (userId: string) => Promise<unknown>
  /** Optional toast hooks; falls back to inline messages when absent. */
  onError?: (message: string) => void
  onSuccess?: (message: string) => void
}

/**
 * A pending or redeemed invitation. Keyed by email because it is minted before
 * the person has an account — that is the whole point of an invitation.
 */
export interface AdminInvitation {
  email: string
  /**
   * What the invitation confers, as an opaque string: a billing tier in one app
   * ("collab", "max"), a plan in the next ("pro"). The package never interprets
   * it — the host supplies the allowed values through `grantOptions` and the
   * labels to render them with.
   */
  grant?: string | null
  /** Why it was granted, when the app distinguishes motives. Free text. */
  reason?: string | null
  invitedAt?: string | Date | null
  expiresAt?: string | Date | null
  /** Set once someone signed up with it. Such rows fold into the account row. */
  usedAt?: string | Date | null
  /**
   * The full link to hand over, built by the backend so the URL is not
   * reassembled from a token on both sides. Present only while the backend can
   * still read the token back; absent means "Copier le lien" is not rendered.
   */
  inviteUrl?: string | null
}

/**
 * Invitations as a port of its own, deliberately not folded into
 * {@link AdminUserApi}: apps that only want a users table implement nothing,
 * and the existing implementors of `AdminUserApi` keep compiling.
 *
 * Same contract over backends that share no code — a Go service in one app, a
 * SQL route in another.
 */
export interface AdminInvitationApi {
  listInvitations(): Promise<{ invitations: AdminInvitation[] }>
  inviteUser(input: { email: string; grant?: string }): Promise<unknown>
  /**
   * Optional: not every app can revoke. When absent the action is not rendered
   * at all, rather than the host wiring a method that throws.
   */
  revokeInvitation?(email: string): Promise<unknown>
  /**
   * Optional: send (or re-send) the invitation email for a row that already
   * exists. Apps that mint the invitation without mailing it wire this so the
   * two steps stay separate — creating an invitation and delivering it fail for
   * different reasons and are worth retrying independently.
   */
  resendInvitation?(email: string): Promise<unknown>
  /**
   * Optional: mint a fresh token and restart the TTL, invalidating the previous
   * link. This is how a short-lived invitation is recovered once it expires,
   * so the row is never a dead end. Does not mail anything on its own.
   */
  regenerateInvitation?(email: string): Promise<unknown>
}

export interface AccountsTableProps {
  api: AdminUserApi
  /** Omit to get exactly the {@link UsersTableProps} behaviour, invitations aside. */
  invitations?: AdminInvitationApi
  /** The grants this app may confer, in the order they should be offered. */
  grantOptions?: { value: string; label: string }[]
  /** See {@link UsersTableProps.onDeleteUser}. */
  onDeleteUser?: (userId: string) => Promise<unknown>
  onError?: (message: string) => void
  onSuccess?: (message: string) => void
}

export interface AdminLoginFormProps {
  authClient: AdminAuthClient
  /** Fetch the fresh profile after sign-in to check the admin role. */
  getProfile: () => Promise<UserProfile>
  /** Called once the user is confirmed admin (app navigates to /admin). */
  onSuccess: () => void | Promise<void>
  /** Optional label overrides. */
  title?: string
  subtitle?: string
  /**
   * Per-string overrides so i18n-enabled apps can translate the form. Defaults
   * are French; pass the keys you need from your own catalogue.
   */
  labels?: Partial<AdminLoginLabels>
  /**
   * Optional leading icon inside the submit button (e.g. a lucide <LogIn/>).
   * Passed as a node so the package needs no icon dependency.
   */
  icon?: ReactNode
  /** Rendered under the card — typically a secondary link. */
  footer?: ReactNode
  /**
   * Extra classes on the form's own wrapper. The component sets a readable
   * default width (`w-full max-w-sm`); pass e.g. `max-w-md` to override it.
   * Centering and page background stay the app's job.
   */
  className?: string
}

/** Inner strings of {@link AdminLoginFormProps}, overridable for i18n. */
export interface AdminLoginLabels {
  email: string
  password: string
  submit: string
  submitting: string
  signInFailed: string
  notAnAdmin: string
}

export interface AdminSetupFormProps {
  /**
   * Create the first admin. App-side (SQL insert before any admin exists).
   * Resolve on success; reject with a message otherwise.
   *
   * `code` is present only when {@link AdminSetupFormProps.onRequestCode} is
   * supplied — it is the value the operator read from their inbox, and the app
   * is what verifies it. This component never decides whether a code is valid.
   */
  onSubmit: (input: {
    name: string
    email: string
    password: string
    code?: string
  }) => Promise<void>
  /**
   * Send a one-time code to the address being registered. Supplying it turns
   * the form into two steps: details first, then the code.
   *
   * Optional on purpose — without it the form stays exactly as it was, so apps
   * that have no mailer wired keep working across the upgrade.
   *
   * It proves the operator can read the mailbox they are claiming. It does NOT
   * protect the endpoint itself: on a setup route reachable with no session, an
   * attacker simply enters an address they own. Gate the route separately.
   */
  onRequestCode?: (email: string) => Promise<void>
  /** Called after a successful creation (app navigates to /login). */
  onSuccess?: () => void | Promise<void>
  title?: string
  subtitle?: string
  /**
   * Per-string overrides so i18n-enabled apps can translate the form. Defaults
   * are French; pass the keys you need from your own catalogue.
   */
  labels?: Partial<AdminSetupLabels>
  /**
   * Optional leading icon inside the submit button (e.g. a lucide <LogIn/>).
   * Passed as a node so the package needs no icon dependency.
   */
  icon?: ReactNode
  /** Rendered under the card — typically a secondary link. */
  footer?: ReactNode
  /**
   * Extra classes on the form's own wrapper. The component sets a readable
   * default width (`w-full max-w-sm`); pass e.g. `max-w-md` to override it.
   * Centering and page background stay the app's job.
   */
  className?: string
}

/** Inner strings of {@link AdminSetupFormProps}, overridable for i18n. */
export interface AdminSetupLabels {
  name: string
  email: string
  password: string
  passwordHint: string
  submit: string
  submitting: string
  created: string
  redirecting: string
  setupFailed: string
  /** Second step, shown only when `onRequestCode` is supplied. */
  code: string
  codeHint: string
  codeSent: string
  sendCode: string
  sendingCode: string
  resendCode: string
  verifyAndCreate: string
  back: string
}
