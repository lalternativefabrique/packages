# @lalternative/lungor-sdk-react

React components for Lungor checkout.

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
`primary`…). No component library is imported, so the picker inherits the host
app's theme — including dark mode — without depending on which shadcn
components that app happens to have copied in.

## Apple Pay

Offered only where the browser can honour it (Safari or iOS, with a card set
up), as its own button above the list per Apple's guidelines. Everywhere else
it is dropped from the list rather than shown and refused later.
