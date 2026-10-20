# @lalternative/legal

Shared legal pages for L'Alternative products: legal notice and cookie policy.

The publisher is one legal entity across every product, so the wording that
describes *who* publishes is written once here. What differs per product —
prices, subprocessors, retention periods — is not in this package and must not
be: a copied subprocessor list is a false declaration under art. 28 GDPR.

## Publisher identity stays out of this package

This package is published to the public npm registry. It ships **no** company
registration number, address or civil name. Each site passes its own `publisher`
object from its own code:

```tsx
import { LegalNotice, type Publisher, type Host } from '@lalternative/legal'

const PUBLISHER: Publisher = {
  name: "L'Alternative Fabrique",
  legalForm: 'entrepreneur individuel (micro-entreprise)',
  address: ['…', '…'],
  registration: '…',
  apeCode: '6201Z',
  publicationDirector: '…',
  vatExempt: true,
}

const HOSTS: Host[] = [
  { name: 'OVH SAS', address: ['2 rue Kellermann', '59100 Roubaix, France'] },
]

export function LegalPage() {
  return (
    <LegalNotice
      publisher={PUBLISHER}
      hosts={HOSTS}
      site={{ product: 'Techtuel', domain: 'techtuel.com', contactEmail: 'contact@techtuel.com' }}
      updatedAt="2026-09-05"
      locale="fr"
      classNames={{ title: 'font-mono text-3xl font-bold text-ink' }}
    >
      {/* product-specific sections go here: AI processing, subprocessors */}
    </LegalNotice>
  )
}
```

## Cookie policy

`strictlyNecessaryOnly` drives the legal conclusion the page states. Pass `true`
only while every listed cookie really is necessary to deliver the service the
user asked for — session, authentication, display preference. Adding any
analytics or advertising cookie ends the consent exemption, and the flag must
flip in the same change that adds the tracker.

```tsx
<CookiesPage
  publisher={PUBLISHER}
  site={SITE}
  hosts={HOSTS}
  updatedAt="2026-09-05"
  strictlyNecessaryOnly
  cookies={[
    { name: 'session_token', purpose: 'Authentification et maintien de session', retention: 'session' },
    { name: 'techtuel_locale', purpose: "Préférence de langue de l'interface", retention: '12 mois' },
  ]}
/>
```

## Pricing terms

The price article of a terms-of-sale page is the same wording for every
product; only the figures differ. `PricingTerms` renders the two sections
(prices and billing, price changes) from those figures and the site drops its
plan table in as children. Nothing about rates or tiers lives in the package.

```tsx
import { PricingTerms } from '@lalternative/legal'

<PricingTerms
  publisher={PUBLISHER}
  site={{ ...SITE, paths: { pricing: '/tarifs' } }}
  locale="fr"
  noticeDays={30}
  usage={[
    { label: 'transcription audio', rate: '2 crédits par minute entamée' },
    { label: 'vidéo déjà sous-titrée', rate: '1 crédit par transcription' },
  ]}
  bestTier
  titles={{ pricing: '4. Prix et facturation', revision: '5. Évolution des tarifs' }}
>
  <PlanPrices catalogue={catalogue} />
</PricingTerms>
```

`usage` is the metered part of the price, when there is one: every rate listed
must be the one the billing system charges. `bestTier` states that the customer
is moved to the cheaper plan matching their consumption without asking; pass it
only when the billing code does exactly that. `noticeDays` is the notice given
before a price change takes effect, with free termination until that date, which
is what keeps the clause out of the consumer-law blacklist on unilateral price
changes. The VAT paragraph follows `publisher.vatExempt`.

## Styling

The package ships structure, not a look. Every class is overridable through
`classNames`, since each site keys on its own palette.

## What is deliberately absent

Terms of sale (beyond the price article above), privacy policy and the DPA are **not** here. They turn on the
product's object, price, subprocessors and retention periods, and on whether the
product acts as controller or processor — Techtuel is a processor for the media
its customers submit, Synthiz a controller for its end users. Sharing that
wording would produce documents that are wrong for at least one product.
