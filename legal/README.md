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

## Styling

The package ships structure, not a look. Every class is overridable through
`classNames`, since each site keys on its own palette.

## What is deliberately absent

Terms of sale, privacy policy and the DPA are **not** here. They turn on the
product's object, price, subprocessors and retention periods, and on whether the
product acts as controller or processor — Techtuel is a processor for the media
its customers submit, Synthiz a controller for its end users. Sharing that
wording would produce documents that are wrong for at least one product.
