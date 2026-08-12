# @lalternative/auth

Shared [Better Auth](https://better-auth.com) wrapper for L'Alternative apps.

Provides platform auth defaults (email-OTP + admin plugins, opt-in magic link),
a React client, and the auth UI forms (login, register, verify-email,
forgot/reset password, magic link, auth layout).

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
import { LoginForm, RegisterForm, SocialButtons, VerifyEmailForm, ForgotPasswordForm, ResetPasswordForm, AuthLayout, useSession, useLogout } from "@lalternative/auth"
```

### Magic link

Passwordless sign-in by emailed link. Off unless `magicLink` is passed — the
`/sign-in/magic-link` route is only mounted when it is, so an app that does not
render the form does not expose the endpoint either.

```ts
export const auth = createPlatformAuth({
  // …
  magicLink: {
    expiresIn: 300,      // default
    allowSignUp: false,  // default
  },
})
```

`allowSignUp` is off on purpose. `createPlatformAuth` requires a verified email
and can be put behind an invite-only beta, and both of those gates gate
`/sign-up/email` — a magic link that creates the account walks past them. Turn
it on only where sign-up is open anyway.

The mailer receives the ready-made URL rather than an OTP:

```ts
const mailer: PlatformAuthMailer = async ({ to, subject, html, type, url }) => {
  // type === "magic-link", url is signed and points at the app's callback
}
```

```tsx
import { MagicLinkForm } from "@lalternative/auth"

<MagicLinkForm authClient={authClient} callbackUrl="/dashboard" />
```

The form confirms that a link was sent, never that the account exists: Better
Auth answers the send identically either way, and only refuses at
`/magic-link/verify`. Distinguishing the two in the form would tell an
anonymous caller which addresses are registered.

That means **every** failure past the send comes back on the callback as
`?error=`, with no component mounted to have caught it — the same shape as an
OAuth round-trip, and read the same way:

```ts
import { initialMagicLinkError, isMagicLinkError } from "@lalternative/auth"

const error = initialMagicLinkError() // undefined unless the code is a magic-link one
```

`initialMagicLinkError` ignores OAuth's codes, and `initialOAuthError` is
unchanged, so a screen offering both flows reads the one `?error=` against each
vocabulary without either claiming the other's failures. `INVALID_TOKEN` covers
expiry and reuse alike: the token is consumed atomically on first use, so a link
followed twice is indistinguishable from one that timed out.

### Invitations

An invitation link lands on the app's own sign-up page
(`/register?invite=<token>`) and is redeemed once the account exists. Claim on
the auth callback, not in the page: a sign-up completes through password + OTP,
OAuth redirect or email verification, and only two of those return to the page
that held the token.

```ts
// server — auth callback (e.g. routes/api/auth/$.ts)
import {
  claimInvitation,
  completesSignup,
  invitationOutcomeCookie,
  inviteTokenFrom,
} from "@lalternative/auth/server"

const response = await auth.handler(request)
if (!response.ok || !completesSignup(new URL(request.url).pathname)) return response

// Read the session back from the Set-Cookie the handler just issued, so this
// works for every flow without parsing each response body shape.
const setCookie = response.headers.get("set-cookie")
const session = setCookie
  ? await auth.api
      .getSession({ headers: new Headers({ cookie: setCookie }) })
      .catch(() => null)
  : null

const token = inviteTokenFrom(request)
if (token && session?.user) {
  // Best-effort: a sign-in must never fail because a claim did not go through.
  const outcome = await claimInvitation({
    endpoint: `${process.env.LUNGOR_API_URL}/invitations/claim`,
    apiKey: process.env.LUNGOR_APP_API_KEY,
    token,
    externalUserId: session.user.id,
  })
  if (outcome !== "granted") {
    response.headers.append("set-cookie", invitationOutcomeCookie(outcome))
  }
}
```

```tsx
// UI — telling the invitee why a link did not work
import { InvitationNotice, isInvitationFailure } from "@lalternative/auth"

if (isInvitationFailure(outcome)) return <InvitationNotice reason={outcome} />
```

`endpoint` is any backend that redeems a token, so an app already claiming
against its own API keeps doing so; `extra` adds fields to the request body.
