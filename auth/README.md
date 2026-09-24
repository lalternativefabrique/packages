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
import { createPlatformAuth, ssoFromEnv } from "@lalternative/auth/server"

export const auth = createPlatformAuth({
  // …
  sso: ssoFromEnv("tornad", {
    URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
    URBANGATE_CLIENT_ID: process.env.URBANGATE_CLIENT_ID,
    URBANGATE_CLIENT_SECRET: process.env.URBANGATE_CLIENT_SECRET,
  }),
})
```

`ssoFromEnv` takes an `SsoEnv`, the three variables it reads and nothing
else — never the whole `process.env` (see [Environment](#environment)). It is
`undefined` while `URBANGATE_CLIENT_SECRET` is unset, so an app boots without
SSO until the secret reaches it. The client is `<product>-admin` and
the admin role `<product>:admin`, which is what urbangate declares for every
product. Pass a `PlatformSsoConfig` by hand only when an app departs from that.

The callback is `/api/auth/callback/urbangate`; register it on the Hydra
client. The provider's endpoints are derived from the issuer, so the app boots
even when the issuer is unreachable; discovery only adds ID-token verification.
On the client, `startSso(authClient, { callbackURL: "/admin" })`
starts the redirect (Better Auth 1.7 serves generic providers through
`signIn.social`, so no client plugin is needed).

### The person's token reaches the core (0.20.0)

urbangate's ADR 0009: a signed-in person reaches the product's core with the
access token Hydra issued to them, not with one the web signs. `ssoFromEnv`
therefore asks for the product as `audience` at sign-in, and `coreProxy`
forwards the stored access token, refreshed at Hydra by Better Auth when it
is about to expire.

```ts
import { coreProxy } from "@lalternative/auth/server"

const proxy = coreProxy(auth, { coreUrl: process.env.CORE_URL, adminOnly: true })

export const Route = createFileRoute("/api/v1/$")({
  server: { handlers: { ANY: ({ request }) => proxy(request) } },
})
```

The proxy answers 401 `sign_in_required` without a session, 403 when
`adminOnly` is set and the person is not one, and otherwise forwards the
request as it is, minus the cookie and hop-by-hop headers. `fallbackToken`
mints a token for an account without an urbangate access token — one opened
before the change, or a local-password account — and is the bridge a product
removes once every session is on the new token. The core verifies both with
`go/websession`.

## Sign-in on the product's screens, urbangate behind (1.0)

urbangate's ADR 0009: the product renders sign-up, sign-in, the e-mail
code, recovery, with the components above, and nothing of Better Auth runs
behind them. Its server drives Kratos' native flows and holds the session
token in a cookie; the core gets the person's own Hydra token.

```ts
// server
import { createUrbangateAuth } from "@lalternative/auth/urbangate"

export const auth = createUrbangateAuth({
  product: "tornad",
  productName: "Tornad",                              // on the mails; defaults to product
  kratosUrl: process.env.KRATOS_PUBLIC_URL,          // http://kratos:4433 in the space
  urbangate: {
    issuerUrl: process.env.URBANGATE_ISSUER_URL,      // https://id.urbangate.dev
    provisioner: { clientId: "tornad-provisioner", clientSecret: process.env.URBANGATE_PROVISIONER_CLIENT_SECRET },
    admin: { clientId: "tornad-admin", clientSecret: process.env.URBANGATE_CLIENT_SECRET },
  },
})
// /api/auth/$ → auth.handler(request)
// /api/v1/$   → auth.coreProxy({ coreUrl: process.env.CORE_API_URL, adminOnly: true })(request)

// browser
import { createUrbangateAuthClient } from "@lalternative/auth/urbangate-client"
export const authClient = createUrbangateAuthClient()   // the prop every form takes
```

Routes the handler serves under `/api/auth/`, all JSON:

| Route | Kratos flow |
|---|---|
| `POST sign-in/email` `{email,password}` | login, password |
| `POST sign-up/email` `{email,password,name?}` | registration, password; answers `verification.flowId` when a code was sent |
| `POST email-otp/send-verification-otp` `{email,type}` | verification, recovery or login by code, per `type` |
| `POST email-otp/verify-email` `{email,otp}` | verification |
| `POST sign-in/email-otp` `{email,otp}` | login by code, second step |
| `POST email-otp/reset-password` `{email,otp,password}` | recovery, then settings with the recovered session; 403 `second_factor_required` when the identity holds one |
| `POST second-factor/verify` `{code,password}` | login at `aal2` (TOTP, or a backup code), then the pending settings flow |
| `POST update-user` `{name}` | settings, profile; the other traits are kept |
| `POST change-password` `{currentPassword,newPassword,revokeOtherSessions?}` | login with `refresh` to re-prove the current password, then settings, password; `DELETE /sessions` when asked |
| `POST sign-out` | logout |
| `GET get-session` | whoami |

Refusals come back as `{ error: { code, status, message? } }`: `invalid_credentials`
401, `invalid_code` 400, `already_registered` 409, `password_refused` 422 with
Kratos' reason, `flow_expired` 410, `account_disabled` and
`second_factor_required` 403, `unavailable` 503. `sign-in/social` answers 501
until Kratos' social providers are wired.

Cookies, all httpOnly and Lax: `<product>_session` holds the Kratos session
token for thirty days, `<product>_token` the person's Hydra access token for
its fifteen minutes, `<product>_flow` the flow a code was sent for, ten
minutes. `getSession(headers)` reads whoami and takes the role off the token
(`<product>:admin` makes an admin). `accessToken(headers)` exchanges the
session at urbangate when the token is missing or within a minute of its end
and hands back the `Set-Cookie` to append; `coreProxy` does that and forwards,
answering 401 `sign_in_required`, 403 `forbidden`, or 503
`identity_provider_unavailable` when urbangate cannot answer — never 401 for
an outage.

A reset for an identity holding a second factor is refused by Kratos'
settings flow at aal1, after the recovery code is spent. The handler keeps
the recovered session and the settings flow (`<product>_flow=settings2fa:…`)
and answers 403 `second_factor_required`; `ResetPasswordForm` then asks for
the authenticator code and finishes through `authClient.secondFactor.verify`.
A Better Auth client has no `secondFactor`, and the form keeps its old
failure message there.

Every flow submit carries `transient_payload: { product, product_name }`,
which urbangate's courier templates read to name the product on the mail.

Kratos must run the native flows for this product's identities and open the
session at registration: `selfservice.flows.registration.after.password.hooks`
and `.code.hooks` carry `- hook: session`. The `-provisioner` client carries
the `urbangate:sessions:exchange` scope and the `-admin` client the
`urn:ietf:params:oauth:grant-type:jwt-bearer` grant, both declared in
urbangate.

### Mobile apps (1.4)

An Expo app uses the same routes on the product's web, through
`@lalternative/auth/urbangate-native`. It replaces `@better-auth/expo`: the
cookies the web sets are kept in SecureStore and replayed by hand, because
React Native has no reliable cookie jar. The web needs no change.

```ts
import * as SecureStore from "expo-secure-store"
import { createUrbangateNativeClient } from "@lalternative/auth/urbangate-native"

export const authClient = createUrbangateNativeClient({
  baseURL: "https://lalter.fr",
  product: "lalter",
  storage: {
    getItem: (k) => SecureStore.getItemAsync(k),
    setItem: (k, v) => SecureStore.setItemAsync(k, v),
    deleteItem: (k) => SecureStore.deleteItemAsync(k),
  },
})
```

It offers the browser client's methods, plus `fetch(path, init)`, which carries
the session to the product's own routes (the core proxy) and keeps the token
it renews, and `cookieHeader()` for a transport that cannot go through `fetch`
(a WebSocket, an audio player). The magic link has no Kratos counterpart here:
sign in with a password or an e-mail code (`emailOtp.sendVerificationOtp` with
`type: "sign-in"`, then `signIn.emailOtp`).

### Environment

Each helper declares the variables it reads — `SsoEnv` for single sign-on,
`ProvisionerEnv` for enrolment, `UrbangateEnv` being both — and the app hands
it those and nothing else. Passing `process.env` whole would give a library
every secret the process holds for the sake of three or four values. A secret
is the switch of what it enables: unset, that part stays off and the app
behaves as before.

| Variable | Enables | Default |
|---|---|---|
| `URBANGATE_ISSUER_URL` | — | `https://id.urbangate.dev` |
| `URBANGATE_PUBLIC_URL` | — | the issuer |
| `URBANGATE_CLIENT_ID` | — | `<product>-admin` |
| `URBANGATE_CLIENT_SECRET` | single sign-on | none |
| `URBANGATE_PROVISIONER_CLIENT_ID` | — | `<product>-provisioner` |
| `URBANGATE_PROVISIONER_CLIENT_SECRET` | enrolment at the provider, and the key relay | none |

`URBANGATE_PUBLIC_URL` is for a dev stack where the front reaches Kratos by
another address than Hydra's issuer; in production the two are the same host.

### Account settings (1.2)

`AccountSettings` is the default settings page: a profile tab (name, address,
password, account deletion through `DeleteAccountSteps`), a billing tab when
`billing` is given, then whatever tabs the product adds through `sections`.

```tsx
<AccountSettings
  client={authClient}
  user={{ name: session.user.name, email: session.user.email }}
  tab={search.tab}
  onTabChange={(tab) => navigate({ search: { tab } })}
  setPasswordHref="/forgot-password"
  deleteAccount={{ endpoint: "/api/account/delete" }}
  billing={<Billing />}
  sections={[{ id: "comptes", label: "Comptes connectés", content: <Connections /> }]}
/>
```

The address change stays off until `emailChange` is passed: it needs the
server's `user.changeEmail` and, with Kratos passwords, the identity kept in
step. Whether the person has a password is read from `client.listAccounts`;
without one, the row links to `setPasswordHref`. `pnpm playground` shows it
under « Paramètres », with a deletion that fails once then succeeds.

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

## Customer passwords at the identity provider (0.14.0, completed in 0.15.0)

From **0.14.0** — and working end to end from **0.15.0**, which relays the
password to the provider instead of dropping it — an app can move its customers' passwords to the suite's
identity provider while keeping its own login screen, its own domain and its
own session. Nothing is enabled by a version bump alone: passwords stay local
until `kratosPasswords` is passed. An app upgrading to 0.14.x never changes
behaviour by accident.

```ts
import { createPlatformAuth, kratosPasswordsFromEnv } from "@lalternative/auth/server"

createPlatformAuth({
  // …
  kratosPasswords: kratosPasswordsFromEnv(
    "spore",
    {
      URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
      URBANGATE_PUBLIC_URL: process.env.URBANGATE_PUBLIC_URL,
      URBANGATE_PROVISIONER_CLIENT_ID: process.env.URBANGATE_PROVISIONER_CLIENT_ID,
      URBANGATE_PROVISIONER_CLIENT_SECRET: process.env.URBANGATE_PROVISIONER_CLIENT_SECRET,
    },
    { onProvisioningDeferred: ({ userId, email }) => queueIdentityRepair(userId, email) },
  ),
})
```

`kratosPasswordsFromEnv` takes a `ProvisionerEnv`, the four variables it
reads and nothing else — never the whole `process.env` (see
[Environment](#environment)). It is `undefined` while
`URBANGATE_PROVISIONER_CLIENT_SECRET` is unset. The client is
`<product>-provisioner` and the customer role `<product>:user`; urbangate
checks that role against the client's own name, so neither can be anything
else.

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

### The password reaches the provider, or the write fails

Kratos holds the password, so every write must reach it: sign-up, the OTP
reset and a password change all relay what the form collected before anything
is stored locally. Kratos hashes it with the hasher its own configuration
declares, so an app cannot hand over one it hashed itself.

If the provider is unreachable the write fails with 503 and nothing changes,
rather than storing a placeholder against a password Kratos never received —
that account could never be opened again, and nothing would say why.

This is the one place the package refuses rather than degrades. Reading
(signing in) fails closed too, but a sign-up that cannot reach the provider
still creates the local account: the person is registered, `identityId` stays
null, and `onProvisioningDeferred` hands the repair to the app. Writing a
password has no such fallback, because a password stored nowhere is not a
state a repair can fix.

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

### Enrolment follows the verified address (0.16.0)

Every way into the app enrols the person — a password sign-up, a magic link,
Google — because the hook is on the user row, not on one route.

It waits for the address to be verified, though. Enrolment is idempotent on
the address, so sending one nobody proved would join this person to the
identity of whoever actually owns it. A password sign-up is created
unverified and confirmed by its OTP a moment later; a social sign-up is
confirmed by the provider, or not at all if it reports the address unverified.
Enrolment therefore happens when the address becomes verified, whenever that
is, and never for an address that stays unverified.

A social sign-up brings no password, so the identity is created without a
credential: that person signs in through their provider, and sets a password
through recovery only if they ever want one.

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
