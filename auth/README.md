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

### Single sign-on (urbangate)

Passing `sso` mounts an OIDC client of the suite's identity provider. A person
whose `roles` claim carries `adminRole` signs in as admin, anyone else as a
plain user. The role is read from the ID token stored on the account, at
creation and again on every sign-in, so a role removed at the provider is
removed here the next time the person signs in.

```ts
export const auth = createPlatformAuth({
  // …
  sso: {
    issuer: "https://id.urbangate.dev",
    clientId: "tornade-admin",
    clientSecret: process.env.URBANGATE_CLIENT_SECRET!,
    adminRole: "tornade:admin",
  },
})
```

The callback is `/api/auth/callback/urbangate`; register it on the Hydra
client. The provider's endpoints are derived from the issuer, so the app boots
even when the issuer is unreachable; discovery only adds ID-token verification.
On the client, `startSso(authClient, { callbackURL: "/admin" })`
starts the redirect (Better Auth 1.7 serves generic providers through
`signIn.social`, so no client plugin is needed).

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

That `?error=` lands on `errorCallbackUrl`, which defaults to the page the form
is on — the one place asking for another link is possible. Better Auth would
otherwise fall back to `callbackUrl`, typically a signed-in destination, where
an auth guard bounces the visitor and drops the error on the way, leaving an
expired link looking like nothing happened at all.

`initialMagicLinkError` ignores OAuth's codes, and `initialOAuthError` is
unchanged, so a screen offering both flows reads the one `?error=` against each
vocabulary without either claiming the other's failures. `INVALID_TOKEN` covers
expiry and reuse alike: the token is consumed atomically on first use, so a link
followed twice is indistinguishable from one that timed out.

`LoginForm` and `RegisterForm` take the same `errorCallbackUrl`, defaulting the
same way — to the page the form is on, the one that reads the code and renders
it. Better Auth would otherwise keep the browser on its own error route, where
nothing shows the code and a refused provider button reads as broken. The
refusal worth naming is `account_not_linked`: `createPlatformAuth` disables
account linking on purpose, so "Continue with Google" on an address already
registered with a password is rejected rather than folded into that account.

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

## Customer passwords at the identity provider (0.14.0)

From **0.14.0**, an app can move its customers' passwords to the suite's
identity provider while keeping its own login screen, its own domain and its
own session. Nothing is enabled by a version bump alone: passwords stay local
until `kratosPasswords` is passed. An app upgrading to 0.14.x never changes
behaviour by accident.

```ts
createPlatformAuth({
  // …
  kratosPasswords: {
    publicUrl: process.env.URBANGATE_PUBLIC_URL!,
    issuer: process.env.URBANGATE_ISSUER_URL!,
    clientId: process.env.URBANGATE_PROVISIONER_CLIENT_ID!,
    clientSecret: process.env.URBANGATE_PROVISIONER_CLIENT_SECRET!,
    role: "spore:user",
    product: "spore",
    onProvisioningDeferred: ({ userId, email }) => queueIdentityRepair(userId, email),
  },
})
```

The login form, its copy and its routes do not change, and nobody is
redirected: the password is posted to this app as before and checked against
Kratos instead of a local hash.

### Each app keeps its own accounts

The same person signing up on two products gets two local users and two
passwords, which may use two different addresses. They are never told the
products know each other. What they share — when the address is the same — is
one identity at the provider, which is what an app key is issued against.

An app therefore **must not deactivate or delete the identity** when it
deletes a local account: it drops its own role and its local row. Deactivating
the identity would sign the person out of every other product of the suite.

The address is what joins the two, and nothing else does. Someone who signs up
on spore with one address and on lalter with another gets **two identities**,
and the provider has no way to know they are the same person. Their app keys
are then split across those identities: `/keys` shows each set on its own, and
a key minted under one cannot name the other's product. That follows from each
app keeping its own accounts, and is not a defect to route around — but an
integrator who used two addresses will meet it, and the answer is to sign up
with the same address on both products.

### Refusals a form must tell apart

`res.error.message` carries the reason, so the existing error banner renders
it with no change. A page that routes rather than renders uses the predicates:

| Predicate | Meaning |
|---|---|
| `needsPasswordRecovery` | The identity has no password yet (an account predating the move). Send to recovery — it is **not** a wrong password. |
| `isIdentityProviderUnavailable` | The provider is unreachable. The password was never refused; do not suggest changing it. |
| `needsSecondFactor` | Kratos requires a second factor. |
| `isAccountDisabled` | The identity is deactivated. |

Verification fails closed: only an explicit refusal by Kratos reads as a wrong
password, and an outage answers 503 so nobody rotates a password that was
right.

### Why the sentinel hash

Better Auth's `/sign-in/email` reads the credential row and refuses **before**
reaching the verifier when it carries no hash, and its verifier is handed only
`{hash, password}` — never the address. So the package writes
`KRATOS_SENTINEL_HASH` in place of a hash and carries the address to the
verifier from the route hook.

The sentinel is a constant, not a hash: argon2/bcrypt/scrypt verification of
it fails on its format, so a build that ever bypassed the custom verifier
refuses everyone rather than admitting anyone.

This is deliberate and it is a workaround. When Better Auth exposes a seam for
an external credential provider, the replacement is to handle `/sign-in/email`
before the native route runs, and the sentinel disappears. That was not taken
now because it means re-implementing session creation, which is where a
mistake becomes an authentication hole.

### Provisioning is not atomic

`user.create.after` runs after the insert commits, so a sign-up cannot be
atomic with the identity it needs. A provider that is down leaves `identityId`
null and the person registered all the same — a customer is never refused
registration because the provider is unavailable. `onProvisioningDeferred`
receives those sign-ups so the app can queue the repair, which re-sends
through `provisionIdentity`. The endpoint is idempotent on the address, so a
repair for someone who already got an identity returns that same one.

### Rollout

Per app, smallest customer base first — never all at once. An app that
switches and breaks locks its customers out.
