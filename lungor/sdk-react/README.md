# @lalternative/lungor-sdk-react

React components for Lungor pricing and checkout.

## PricingTable

Renders an app's catalogue and hands back the plan a shopper picked. Every
figure comes from the rows you pass, which are Lungor's own — a page that
restated a price in its markup would eventually disagree with the checkout that
charges it.

```tsx
import { PricingTable } from '@lalternative/lungor-sdk-react';

// plans come from Lungor's GET /finance/plans — your backend reads it with the
// app key, or the browser reads the public catalogue for a given app id.
<PricingTable
  plans={plans}
  authenticated={Boolean(user)}
  currentPlanCode={user?.planCode}
  heading="Nos offres"
  formatUnit={({ unit, amount }) => `${amount} ${unit === 'credit' ? 'crédits' : unit}`}
  onSelect={(plan, intent) => {
    if (intent === 'signup') router.navigate({ to: '/signup', search: { plan: plan.code } });
    else startCheckout({ planId: plan.id });
  }}
/>;
```

`onSelect` says what the button should do rather than doing it: the app key that
authorises a checkout is a server-side secret, so your own backend opens one.

- **`intent: 'signup'`** for a free plan or an anonymous visitor. A plan
  charging nothing cannot go through checkout at all — there is no PSP
  credential to open a payment session with — so it is granted after sign-up via
  `POST /finance/subscriptions/grant`.
- **`intent: 'checkout'`** for a signed-in visitor buying a paid plan.
- A plan with `purchasable: false`, or the one named by `currentPlanCode`, gets
  no button rather than one that leads to a refusal.

Set `layout="table"` for a row-by-row comparison, which reads best at two or
three plans; the default `"cards"` grid holds up at any count.

Units are data, never hard-coded: pass `formatUnit` to render `credit`,
`synthesis` or `email` the way your shoppers read it. A plan's `description` is
its one-line tagline under the name ("Pour une veille personnelle régulière."),
and `features` are its selling points, each rendered with a check mark.

In the `cards` layout every plan sits on one row from the `md` breakpoint, so
the container's width decides how much room each card gets.

Every label has a French default and is overridable through `labels`, `heading`
and `description`.

## CheckoutMethodPicker

```tsx
import { CheckoutMethodPicker } from '@lalternative/lungor-sdk-react';

// methods come from YOUR backend, which proxies Lungor's
// GET /finance/checkout/methods?plan_id=… — the app key that
// authorises it is a server-to-server secret and must never
// reach the browser.
<CheckoutMethodPicker
  methods={methods}
  amountLabel="19,00 € / mois"
  busy={isRedirecting}
  refusal={checkoutError?.code}
  onSelect={(paymentMethod, { buyerKind }) => startCheckout({ planId, paymentMethod, buyerKind })}
/>;
```

`onSelect` hands back the method id verbatim; send it as `payment_method` on
`POST /finance/checkout`, then redirect to the `redirect_url` you get back.

The payer also says who they order as — « Particulier » (`consumer`, the
default) or « Professionnel » (`business`) — and the selection carries it as
`buyerKind`, to send as `buyer_kind`. The invoice follows: a consumer gets the
mediator and the 14-day withdrawal notes, a business the B2B settlement terms.
An offer for consumers only refuses a business buyer with a 422 whose `error`
is `business_buyers_not_accepted`; hand that code back as `refusal` and the
picker shows « Cette offre est réservée aux particuliers. » under the choice.
`checkoutRefusalMessage(code)` renders the same text for a page with its own
markup. Every label is overridable through `labels`.

## BillingPage

The one billing page every app mounts on `BILLING_PATH` (`/billing`), and the
one every checkout returns to. Three things, top to bottom: how the checkout
the user just returned from ended, the plan they hold, and the plans they can
move to. The app supplies data and callbacks; the page is the same everywhere.

```tsx
import { BillingPage } from '@lalternative/lungor-sdk-react';

<BillingPage
  plans={plans}                       // GET /finance/plans, through your backend
  subscription={entitlement}          // GET /entitlements, through your backend
  fetchSession={(id) => api.getCheckoutSession(id)}
  onCheckout={(plan) => startCheckout(plan.id)}
  onChangePlan={(plan) => changePlan(plan.code)}
  onCancel={cancel}
  onResume={resume}
  onWithdrawPendingPlan={withdraw}
  onPaid={() => queryClient.invalidateQueries({ queryKey: ['entitlement'] })}
  onContinue={() => router.navigate({ to: '/app' })}
  onLeaveCheckoutReturn={() => router.navigate({ to: '/billing', search: {} })}
/>;
```

`formatUnit` renders an allowance on the grid ("100 crédits par mois");
`formatBalance` renders what remains of a unit on the current plan ("12
crédits"). They are kept apart because a balance is not "per period".

`subscription` is Lungor's entitlement read as is: `entitled`, `status`,
`planCode`, the period, `cancelAtPeriodEnd`, and the pending plan with its
date. Everything the page shows travels on that read, so the app keeps no
subscriptions row of its own.

The page is authenticated: a checkout needs a user to attach the payment to.
The public pricing page hands a chosen plan over as `?plan=<code>` after
sign-up, and the page opens its checkout on arrival.

## CheckoutOutcome

The provider's redirect says nothing about the outcome: Mollie sends the payer
back to `success_url` paid or refused alike. So Lungor stamps
`lungor_session_id=<id>` on both return URLs, and the page they land on asks
how it ended. This component renders nothing off a checkout return, so it can
sit permanently on that page.

```tsx
import { CheckoutOutcome } from '@lalternative/lungor-sdk-react';

// fetchSession calls YOUR backend, which proxies Lungor's
// GET /finance/checkout/{session_id} with the app key.
<CheckoutOutcome
  fetchSession={(id) => api.getCheckoutSession(id)}
  onPaid={() => queryClient.invalidateQueries({ queryKey: ['entitlement'] })}
  onContinue={() => router.navigate({ to: '/app' })}
  onRetry={() => router.navigate({ to: '/pricing' })}
/>;
```

Send the checkout back to the **plans page**, never to the home page: it is
the one page where every ending makes sense. Paid, the grid shows the new
current plan; refused or abandoned, the grid sits right under the message and
the customer tries again without navigating. There, use `variant="hero"`,
which heads the page; the default `banner` fits inside one.

```tsx
<CheckoutOutcome
  variant="hero"
  fetchSession={(id) => api.getCheckoutSession(id)}
  onPaid={() => queryClient.invalidateQueries({ queryKey: ['entitlement'] })}
  onContinue={() => router.navigate({ to: '/app' })}
  onRetry={() => document.getElementById('plans')?.scrollIntoView({ behavior: 'smooth' })}
  onDismiss={() => router.navigate({ to: '/plans', search: {} })}
/>;
```

It polls while the status is `pending` or `redirected`: the provider's
notification can land a few seconds after the payer does, and a page that read
`redirected` as a failure would refuse someone whose card was accepted. Access
is opened on `paid`, never on the redirect alone.

| Status | Shown | Button |
|---|---|---|
| `pending`, `redirected` | « Vérification du paiement… » | — |
| `completed` | « Paiement confirmé » | `onContinue` |
| `failed` | « Paiement refusé » + the reason, when it is one the payer can act on | `onRetry`, `onDismiss` |
| `canceled` | « Paiement annulé » | `onRetry`, `onDismiss` |
| `expired` | « Session expirée » | `onRetry`, `onDismiss` |
| still in flight after `timeoutMs` (20s) | « Confirmation en attente » | asks again, `onDismiss` |

`useCheckoutReturn` is the same logic without the markup, for a page that
renders its own. `readCheckoutSessionId()` reads the id from the URL on its own.

## Styling

Tailwind on the shadcn design tokens (`bg-background`, `border-input`, `ring`,
`primary`…). No component library is imported, so these components inherit the
host app's theme — including dark mode — without depending on which shadcn
components that app happens to have copied in. React is the only runtime
dependency.

The package ships class names, not CSS: the host app's Tailwind build has to
see them. Tailwind's source detection honours `.gitignore`, so `node_modules`
is never scanned on its own and every class the host app does not already use
elsewhere gets purged — the grid falls back to whatever columns survived.
Declare the package as a source in your stylesheet:

```css
@import "tailwindcss";
@source "../node_modules/@lalternative/lungor-sdk-react/dist";
```

## Apple Pay

Offered only where the browser can honour it (Safari or iOS, with a card set
up), as its own button above the list per Apple's guidelines. Everywhere else
it is dropped from the list rather than shown and refused later.
