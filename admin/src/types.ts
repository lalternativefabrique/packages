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

export interface AdminLoginFormProps {
  authClient: AdminAuthClient
  /** Fetch the fresh profile after sign-in to check the admin role. */
  getProfile: () => Promise<UserProfile>
  /** Called once the user is confirmed admin (app navigates to /admin). */
  onSuccess: () => void | Promise<void>
  /** Optional label overrides. */
  title?: string
  subtitle?: string
}

export interface AdminSetupFormProps {
  /**
   * Create the first admin. App-side (SQL insert before any admin exists).
   * Resolve on success; reject with a message otherwise.
   */
  onSubmit: (input: {
    name: string
    email: string
    password: string
  }) => Promise<void>
  /** Called after a successful creation (app navigates to /login). */
  onSuccess?: () => void | Promise<void>
  title?: string
  subtitle?: string
}
