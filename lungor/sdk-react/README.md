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
`synthesis` or `email` the way your shoppers read it.

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
  onSelect={(paymentMethod) => startCheckout({ planId, paymentMethod })}
/>;
```

`onSelect` hands back the method id verbatim; send it as `payment_method` on
`POST /finance/checkout`, then redirect to the `redirect_url` you get back.

## Styling

Tailwind on the shadcn design tokens (`bg-background`, `border-input`, `ring`,
`primary`…). No component library is imported, so these components inherit the
host app's theme — including dark mode — without depending on which shadcn
components that app happens to have copied in. React is the only runtime
dependency.

## Apple Pay

Offered only where the browser can honour it (Safari or iOS, with a card set
up), as its own button above the list per Apple's guidelines. Everywhere else
it is dropped from the list rather than shown and refused later.
