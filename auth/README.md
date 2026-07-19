# @lalternative/auth

Shared [Better Auth](https://better-auth.com) wrapper for L'Alternative apps.

Provides platform auth defaults (email-OTP + admin plugins), a React client,
and the auth UI forms (verify-email, forgot/reset password, auth layout).

## Install

```bash
pnpm add @lalternative/auth better-auth react react-dom
```

The package is published to the public npmjs.org registry — install is
anonymous, no `.npmrc` override or token needed.

## Usage

```ts
// server (e.g. lib/auth.ts)
import { createPlatformAuth } from "@lalternative/auth/server"

export const auth = createPlatformAuth({ database, secret, /* ... */ })
```

```ts
// client (e.g. lib/auth-client.ts)
import { createPlatformAuthClient } from "@lalternative/auth/client"

export const authClient = createPlatformAuthClient({ baseURL })
```

```tsx
// UI + hooks
import { VerifyEmailForm, ForgotPasswordForm, ResetPasswordForm, AuthLayout, useSession, useLogout } from "@lalternative/auth"
```
