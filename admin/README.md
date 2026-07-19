# @lalternative/admin

Shared admin back-office UI for L'Alternative apps: React components and hooks
that sit on top of the [Better Auth](https://better-auth.com) `admin` plugin
already wired through [`@lalternative/auth`](../auth).

Every app shares the same auth wrapper, so the admin surface (list users,
delete, ban/unban, promote/demote) is identical everywhere — this package holds
it once. Apps mount the components in their own `/admin/*` routes and pass a
small capability adapter (`AdminUserApi`); the router guard and the server-side
delete/setup routes stay in each app.

## Install

```bash
pnpm add @lalternative/admin react react-dom
```

Published to the public npmjs.org registry — install is anonymous, no `.npmrc`
override or token needed.

## Why a capability interface, not the auth client

`@lalternative/auth`'s `createPlatformAuth` widens its return to the base
`Auth` type, which hides the `admin()` plugin's `role`/`banned` fields from the
static types. So this package depends on a hand-written `AdminUserApi`
interface, not on the auth client type. Each app writes a one-line adapter from
its `authClient.admin.*` to `AdminUserApi`. The package needs neither
`better-auth` nor a router as a dependency.

## Usage

```tsx
// apps/<app>/src/routes/admin/users.tsx
import { UsersTable } from "@lalternative/admin"
import { authClient } from "@/lib/auth-client"

const api = {
  listUsers: (q) => authClient.admin.listUsers({ query: q }).then((r) => r.data ?? { users: [] }),
  banUser: (id, reason) => authClient.admin.banUser({ userId: id, banReason: reason }),
  unbanUser: (id) => authClient.admin.unbanUser({ userId: id }),
  setRole: (id, role) => authClient.admin.setRole({ userId: id, role }),
}

export const Route = createFileRoute("/admin/users")({
  component: () => (
    <UsersTable
      api={api}
      onDeleteUser={(id) =>
        fetch(`/api/admin/users/${id}`, { method: "DELETE", credentials: "include" })
      }
    />
  ),
})
```

```tsx
// components + hooks
import {
  AdminLayout,
  AdminLoginForm,
  AdminHome,
  UsersTable,
  AdminSetupForm,
  useIsAdmin,
  hasAdminFeatures,
} from "@lalternative/admin"
```

The route guard (`beforeLoad` + redirect) stays in the app — it is coupled to
the app's router. `hasAdminFeatures(profile)` is the shared rule for it.
