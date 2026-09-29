# @lalternative/auth

The suite's sign-in for L'Alternative apps, on urbangate: the product's
server routes (`createUrbangateAuth`), the browser and Expo clients, and the
auth UI (login, register, e-mail code, forgot and reset password, account
settings, auth layout).

**2.0** drops the Better Auth mode: `@lalternative/auth/server`,
`@lalternative/auth/client`, `createPlatformAuth`, `createPlatformAuthClient`,
`useSession`, `useLogout`, `startSso`, `MagicLinkForm` and the `Platform*`
types are gone, and `better-auth` is no longer a peer dependency. Every
product signs in through urbangate; the sections below are the whole API.

## Install

```bash
pnpm add @lalternative/auth react react-dom
```

The package is published to the public npmjs.org registry — install is
anonymous, no `.npmrc` override or token needed.

```tsx
// UI
import { LoginForm, RegisterForm, VerifyEmailForm, ForgotPasswordForm, ResetPasswordForm, AccountSettings, AuthLayout } from "@lalternative/auth"
```

## Sign-in on the product's screens, urbangate behind (1.0)

urbangate's ADR 0009: the product renders sign-up, sign-in, the e-mail
code, recovery, with the components above. Its server drives Kratos' native flows and holds the session
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

`cookie: { domain }` (1.10) shares the session across an apex domain's
sub-domains: a product served on `app.messag.eco` behind the same web that
serves `messag.eco` sets `domain: ".messag.eco"`, and a person signed in on
either host is signed in on both. It defaults unset, one host per cookie.
Leave it unset in dev, where the hosts differ. The value is meant to come
from configuration and is refused unless it is a plain hostname (1.10.2), so
it can never smuggle attributes into the `Set-Cookie` header. Every
sub-domain of the apex then receives the session cookie, so point it only at
an apex whose sub-domains you control.

A core whose public routes share the proxied path — invitation claims,
app-key calls, a payment provider's callbacks — passes `anonymous: "forward"`
(1.5): a request without a session reaches the core as it came, keeping its own
`Authorization` header, and a signed-in one still gets the person's token.
`adminOnly` keeps turning signed-in non-admins away. An unreachable core
answers 502 `core_unavailable`.

`getSession` reads the role off a token that is still valid: when the cookie's
has expired it exchanges the session first, and `get-session` sets the new
cookie. While urbangate cannot be reached the person reads as a plain user,
never as the admin an expired token once said they were.

A reset for an identity holding a second factor is refused by Kratos'
settings flow at aal1, after the recovery code is spent. The handler keeps
the recovered session and the settings flow (`<product>_flow=settings2fa:…`)
and answers 403 `second_factor_required`; `ResetPasswordForm` then asks for
the authenticator code and finishes through `authClient.secondFactor.verify`.
A client without `secondFactor` keeps the form's old
failure message there.

Every flow submit carries `transient_payload: { product, product_name }`,
which urbangate's courier templates read to name the product on the mail.

Kratos must run the native flows for this product's identities and open the
session at registration: `selfservice.flows.registration.after.password.hooks`
and `.code.hooks` carry `- hook: session`. The `-provisioner` client carries
the `urbangate:sessions:exchange` scope and the `-admin` client the
`urn:ietf:params:oauth:grant-type:jwt-bearer` grant, both declared in
urbangate.

### The console signs in through urbangate (1.6)

Customers never see urbangate's page; the suite's team does, for a
product's console (urbangate ADR 0003). `sso` mounts that sign-in on the
`admin` client:

```ts
createUrbangateAuth({
  // …
  sso: { appUrl: process.env.APP_URL },   // loginPath "/admin/login", landingPath "/admin"
})

// /admin/login
<AdminLoginForm
  authClient={authClient}
  getProfile={getProfile}
  sso={{ signIn: () => authClient.signIn.urbangate({ callbackURL: "/admin" }), only: true }}
/>
```

| Route | |
|---|---|
| `GET sign-in/urbangate?callbackURL=` | redirects to Hydra, authorization code with PKCE, audience the product |
| `GET callback/urbangate` | trades the code, then lands on `callbackURL`, or on `loginPath?error=` |

The client needs the `authorization_code` and `refresh_token` grants and
`<appUrl>/api/auth/callback/urbangate` in its `redirect_uris`, as urbangate
declares for every `-admin` client. The errors on `loginPath` are
`sso_state` (the sign-in was started elsewhere or too long ago),
`sso_refused`, `not_admin` and `unavailable`.

With `sso`, `admin` comes from that sign-in only: a session opened on the
product's screens reads as `user` whatever its token carries. The cores do
not make that distinction yet: a token exchanged from such a session still
carries `<product>:admin` until urbangate's token hook drops it on the
`jwt-bearer` grant.

The console session is Hydra's refresh token in `<product>_admin`, rotated
at each renewal. The access token is verified against Hydra's JWKS, as the
cores verify it, so a token cookie the browser forged is no session. The
name and address come from `/userinfo` when the token is issued or renewed,
and are kept in `<product>_profile` in between. The console's token and that
profile are sealed with the `-admin` client's secret in `<product>_seal`, so
neither a token exchanged from a customer session beside a made-up
`<product>_admin`, nor an address edited in the browser, passes as the
console's own. While Hydra cannot answer, `get-session` is 503, never a
signed-out `null`.

A renewal sets two cookies, so a route that hands `accessToken()`'s cookies
back appends every one of `setCookies`, not `setCookie` alone: a refresh
token dropped on the floor is spent, and the console signs out once Hydra's
`rotation_grace_period` (60 s at urbangate) has passed. That same grace is
what lets two replicas renew one refresh token at once; within a process,
concurrent renewals share one call.

### The routes every product used to write (1.6)

| Route | Answers |
|---|---|
| `GET core-token` | renews the person's token when it is missing or near its end, and sets every cookie; 401 `sign_in_required`, 503 while urbangate cannot answer |
| `GET profile` | `{ user_id, email, name, avatar_url, roles: [role] }`, the `UserProfile` `@lalternative/admin` reads; 401 signed out |

`coreTokenInBody: true` makes `core-token` also answer
`{ token, expires_at }`, for a client that is no browser, a CLI, and needs
the bearer itself. It stays off unless a product has such a client: in a
browser it hands the token to any script on the page.

A product whose browser reaches its core directly calls `core-token` on the
core's 401 and retries; `AdminLoginForm`'s `getProfile` fetches `profile`.
Neither needs a route of the product's own any more.

### Mobile apps (1.4)

An Expo app uses the same routes on the product's web, through
`@lalternative/auth/urbangate-native`: the
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

The app reaches its core only through `authClient.fetch` on the web's core
proxy: the proxy attaches the person's token, so the device never holds one,
and needs neither `core-token` nor a `token` cookie of its own.

### What the product's server no longer writes (1.7)

```ts
export const auth = createUrbangateAuth({
  // …
  coreUrl: process.env.CORE_URL,                       // http://core:4100 in the space
  onAccountOpened: async ({ user, headers, request }) => {
    const call = await auth.coreFetch(headers, "/api/v1/llm-usage/provision", { method: "POST" })
    return call.status === "ok" ? call.setCookies : []
  },
})

// /api/core/$ → auth.coreProxy({ stripPrefix: "/api/core", forwardCookies: ["txl_trial"] })(request)
// a server route  → const g = await auth.requireAdmin(request.headers); if ("response" in g) return g.response
// as the person   → const call = await auth.coreFetch(request.headers, "/me"); if (call.status !== "ok") return coreRefusal(call)
```

| | |
|---|---|
| `coreProxy({ stripPrefix, forwardCookies })` | `coreUrl` defaults to the auth's. The prefix is cut at a segment boundary, and a path that does not start with it, or that would leave the core's origin, is a 404. The cookies named are the only ones the core sees; naming one of the auth's own is refused at build. An event stream is relayed unbuffered, every `Set-Cookie` of the core is kept, and the body's decoded `content-encoding` is dropped |
| `coreFetch(headers, path, init)` | `ok` with the response and the cookies to set, `signed_out`, or `unavailable` with `cause` `identity_provider` or `core`; `coreRefusal(call)` answers 401, 503 or 502 |
| `requireSession(headers)`, `requireAdmin(headers)` | `{ session, setCookies }`, or `{ response }`: 401 signed out, 403 not this product's admin, 503 while urbangate cannot answer, including when it cannot say which roles the person holds |
| `onAccountOpened` | runs once an identity created here has proven its address: the code that verifies a password sign-up, a code that signs an unknown address up, or a sign-up under an owned domain (1.11); never on a sign-in, never before the proof, so an invitation claimed there went to its mailbox's owner |

`adminOnly` on the proxy follows the same rule: a 503, not a 403, while the
roles cannot be read. A route guard in `beforeLoad` runs these through the
framework's server function; it is the server call that protects, the
navigation only follows it.

`EmailCodeSignInForm` is the sign-in by e-mail code, both steps, with a link
back to the password form; an unknown address is signed up by the same code.

### A domain the product owns (1.11)

A product that mints its own mailboxes (messag on `@messag.eco`) has nowhere
to send a sign-up code: the address does not exist until the account does.
Declare the domain, and a sign-up under it takes another road, the one
urbangate ADR 0013 describes:

```ts
export const auth = createUrbangateAuth({
  // …
  coreUrl: process.env.CORE_URL,
  ownedDomains: ["messag.eco"],
})
```

1. `GET <coreUrl>/api/v1/machine/identifiers/<local>` on the product's core,
   which holds the members and the reserved names (`go/membership`): 409 is
   `already_registered`, 400 is `identifier_refused` with the core's reason,
   and nothing has been created.
2. `POST /api/machine/identities` at urbangate with `email_verified: true`
   and `exclusive: true`: the identity is provisioned verified, no
   verification flow, no mail. An address already enrolled is
   `already_registered` before any role is added or password set — a
   sign-up never joins an existing identity under an owned domain, or anyone
   could set the password of an address they do not own.
3. The password is set through `PUT /api/machine/passwords`, the session is
   opened by a Kratos login, and `onAccountOpened` runs at once: the address
   is proven by construction.

Any other address on the same product keeps the registration flow and its
code. The core must be reachable during the sign-up; it answers 503
`unavailable` otherwise, and the person retries.

### A dev stack without urbangate (1.8)

A product's dev stack does not run urbangate: `createDevAuth` stands in for
`createUrbangateAuth` with the same `UrbangateAuth` surface, routes and
cookies, so the browser and Expo clients, the core proxy and the route guards
work unchanged. Any address and password (or any e-mail code) open a session;
the tokens the core receives are EdDSA, signed by the web itself, and the key
set is served at `/api/auth/jwks`.

```ts
import { createDevAuth, createUrbangateAuth } from "@lalternative/auth/urbangate"

export const auth =
  process.env.URBANGATE_DEV_AUTH === "1"
    ? createDevAuth({
        product: "messag",
        issuer: "http://web:5273",                     // the web as the core reaches it
        coreUrl: process.env.CORE_URL,
        admins: process.env.URBANGATE_DEV_ADMINS?.split(","),
      })
    : createUrbangateAuth({ /* … */ })
```

The core trusts that issuer through `go/websession`'s `Web`
(`websession.ConfigFromEnv` reads it from `OIDC_WEB_ISSUER_URL`). It trusts
whoever asks: set the variables in the dev stack only. Password checks, codes,
second factors and account recovery all succeed without checking anything,
and `admins` is the only way to an admin session. Its signing key is derived
from the product's name, so anyone can mint its tokens. Two guards keep it
out of production: `createDevAuth` throws under `NODE_ENV=production`, and
`go/websession` refuses a core that names the web issuer beside urbangate.

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
} from "@lalternative/auth/urbangate"

const response = await auth.handler(request)
if (!response.ok || !completesSignup(new URL(request.url).pathname)) return response

const setCookie = response.headers.get("set-cookie")
const session = setCookie
  ? await auth.getSession(new Headers({ cookie: setCookie })).catch(() => null)
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
