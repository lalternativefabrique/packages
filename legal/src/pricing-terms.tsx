import type { ReactNode } from 'react'
import { Section, type LegalClassNames } from './shell'
import { resolvePaths, type Locale, type Publisher, type Site } from './types'

export type UsageUnit = {
  /** What is metered, e.g. "transcription audio". */
  label: string
  /** The rate for that unit, e.g. "2 crédits par minute entamée". */
  rate: string
}

export type PricingTermsProps = {
  publisher: Publisher
  site: Site
  locale?: Locale
  /**
   * Metered units and their rates when part of the price follows consumption.
   * Omit for flat subscriptions. A rate stated here must match what the billing
   * system actually charges: a wrong rate is a false price.
   */
  usage?: UsageUnit[]
  /** Whether the customer is moved to the cheaper tier matching their consumption without asking. */
  bestTier?: boolean
  /** Days between the notice of a price change and its effect. 30 is the floor the sites use. */
  noticeDays: number
  /** Section titles, so a site can number them to match its own article order. */
  titles?: { pricing?: string; revision?: string }
  classNames?: LegalClassNames
  /** The plan table or any product-specific pricing content, rendered after the price paragraphs. */
  children?: ReactNode
}

const copy = {
  fr: {
    pricing: 'Prix et facturation',
    revision: 'Évolution des tarifs',
    offers: (product: string) =>
      `Les offres payantes de ${product} sont des abonnements mensuels, facturés d'avance, en euros. Les prix applicables sont ceux affichés sur la page Tarifs au moment de la souscription ; ils proviennent du catalogue de facturation de l'éditeur et font foi entre les parties.`,
    pricingPage: 'page Tarifs',
    usageIntro:
      "Une part du prix est variable et calculée selon votre consommation effective, mesurée par l'éditeur sur la base des unités suivantes :",
    usageProof:
      "Le relevé de consommation établi par les systèmes de l'éditeur fait foi entre les parties, sauf preuve contraire.",
    bestTier:
      "Lorsque votre consommation relève d'une offre moins onéreuse que celle souscrite, l'éditeur applique automatiquement cette offre à compter de la période de facturation suivante.",
    vatExempt:
      "L'éditeur relevant de la franchise en base de TVA (art. 293 B du CGI), les prix affichés ne comportent pas de TVA. S'il venait à cesser de relever de ce régime, les prix seraient majorés de la taxe applicable ; cette majoration, de nature fiscale, vous serait notifiée dans les conditions prévues ci-dessous.",
    vat: 'Les prix sont indiqués hors taxes et majorés de la TVA au taux en vigueur.',
    revisionBody: (days: number) =>
      `L'éditeur peut réviser ses tarifs, le tarif des unités de consommation ou les volumes inclus dans une offre. Toute augmentation tarifaire ou réduction substantielle des volumes inclus vous est notifiée par e-mail au moins ${days} jours avant son entrée en vigueur, et ne prend effet qu'à compter de la période de facturation suivante. Vous pouvez résilier sans frais avant cette date. Aucune révision n'affecte une période déjà payée. La poursuite de l'utilisation après l'entrée en vigueur vaut acceptation.`,
  },
  en: {
    pricing: 'Prices and billing',
    revision: 'Price changes',
    offers: (product: string) =>
      `Paid ${product} plans are monthly subscriptions, billed in advance, in euros. The applicable prices are those shown on the Pricing page at the time of subscription; they come from the publisher's billing catalogue and are binding between the parties.`,
    pricingPage: 'Pricing page',
    usageIntro:
      'Part of the price is variable and calculated from your actual consumption, metered by the publisher on the following units:',
    usageProof:
      "The consumption record produced by the publisher's systems is binding between the parties, unless proven otherwise.",
    bestTier:
      'When your consumption falls within a cheaper plan than the one subscribed, the publisher applies that plan automatically from the next billing period.',
    vatExempt:
      'The publisher is exempt from VAT (art. 293 B of the French General Tax Code), so the displayed prices carry no VAT. Should it leave this regime, prices would be increased by the applicable tax; this tax-driven increase would be notified under the conditions set out below.',
    vat: 'Prices are stated excluding tax and increased by VAT at the applicable rate.',
    revisionBody: (days: number) =>
      `The publisher may revise its prices, the rate of consumption units or the volumes included in a plan. Any price increase or substantial reduction of included volumes is notified to you by e-mail at least ${days} days before it takes effect, and applies only from the next billing period. You may terminate free of charge before that date. No revision affects a period already paid. Continued use after the effective date counts as acceptance.`,
  },
}

export function PricingTerms({
  publisher,
  site,
  locale = 'fr',
  usage,
  bestTier = false,
  noticeDays,
  titles,
  classNames,
  children,
}: PricingTermsProps) {
  const t = copy[locale]
  const paths = resolvePaths(site)
  const c = classNames
  const [offersBefore, offersAfter] = t.offers(site.product).split(t.pricingPage)

  return (
    <>
      <Section title={titles?.pricing ?? t.pricing} classNames={c}>
        <p>
          {offersBefore}
          <a className={c?.link ?? 'underline underline-offset-2'} href={paths.pricing}>
            {t.pricingPage}
          </a>
          {offersAfter}
        </p>
        {children}
        {usage && usage.length > 0 && (
          <>
            <p>{t.usageIntro}</p>
            <ul className={c?.list ?? 'list-disc space-y-1 pl-5'}>
              {usage.map((unit) => (
                <li key={unit.label}>
                  {unit.label} : {unit.rate}
                </li>
              ))}
            </ul>
            <p>{t.usageProof}</p>
          </>
        )}
        {bestTier && <p>{t.bestTier}</p>}
        <p>{publisher.vatExempt ? t.vatExempt : t.vat}</p>
      </Section>

      <Section title={titles?.revision ?? t.revision} classNames={c}>
        <p>{t.revisionBody(noticeDays)}</p>
      </Section>
    </>
  )
}
