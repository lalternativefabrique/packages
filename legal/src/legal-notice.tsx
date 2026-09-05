import type { ReactNode } from 'react'
import { LegalShell, Section, formatUpdated, type LegalClassNames } from './shell'
import { resolvePaths, type LegalContext } from './types'

export type LegalNoticeProps = LegalContext & {
  /** Product-specific blocks: hosting detail, AI processing, subprocessors. */
  children?: ReactNode
  classNames?: LegalClassNames
}

const copy = {
  fr: {
    title: 'Mentions légales',
    publisherTitle: 'Éditeur',
    publishedBy: (domain: string, name: string) =>
      `Le site ${domain} et le service associé sont édités par ${name}.`,
    legalForm: 'Forme juridique',
    office: 'Siège social',
    registration: 'SIRET',
    ape: 'Code APE',
    director: 'Directeur de la publication',
    phone: 'Téléphone',
    contact: 'Contact',
    vatExempt: 'TVA non applicable, art. 293 B du CGI.',
    vatNumber: 'Numéro de TVA intracommunautaire',
    hostTitle: 'Hébergement',
    hostIntro: 'Le service et les données sont hébergés par :',
    ipTitle: 'Propriété intellectuelle',
    ip: "L'ensemble des contenus du site — textes, graphismes, logiciels, images, logos, marques et bases de données — est protégé par le droit d'auteur et le droit de la propriété intellectuelle. Toute reproduction, représentation ou modification, totale ou partielle, par quelque procédé que ce soit, est interdite sans autorisation écrite préalable de l'éditeur.",
    liabilityTitle: 'Responsabilité',
    liability:
      "L'éditeur s'efforce d'assurer l'exactitude et la mise à jour des informations diffusées sur le site, sans pouvoir en garantir l'exhaustivité. Il ne saurait être tenu responsable des dommages résultant d'une intrusion frauduleuse d'un tiers, ni du contenu des sites tiers vers lesquels des liens hypertextes renvoient, sur lesquels il n'exerce aucun contrôle.",
    dataTitle: 'Données personnelles',
    data: (privacy: string, cookies: string) => (
      <>
        Le traitement des données personnelles est détaillé dans la{' '}
        <a href={privacy}>politique de confidentialité</a>, et l'usage des cookies dans la{' '}
        <a href={cookies}>politique cookies</a>.
      </>
    ),
    lawTitle: 'Droit applicable',
    law: "Les présentes mentions légales sont soumises au droit français. À défaut de résolution amiable, les tribunaux français sont compétents.",
  },
  en: {
    title: 'Legal notice',
    publisherTitle: 'Publisher',
    publishedBy: (domain: string, name: string) =>
      `${domain} and the associated service are published by ${name}.`,
    legalForm: 'Legal form',
    office: 'Registered office',
    registration: 'Registration number',
    ape: 'Activity code',
    director: 'Publication director',
    phone: 'Phone',
    contact: 'Contact',
    vatExempt: 'VAT not applicable, art. 293 B of the French tax code.',
    vatNumber: 'EU VAT number',
    hostTitle: 'Hosting',
    hostIntro: 'The service and its data are hosted by:',
    ipTitle: 'Intellectual property',
    ip: 'All content on this site — text, graphics, software, images, logos, trade marks and databases — is protected by copyright and intellectual property law. Any reproduction, display or modification, in whole or in part, by any means, is prohibited without the publisher’s prior written consent.',
    liabilityTitle: 'Liability',
    liability:
      'The publisher strives to keep the information on this site accurate and current, without guaranteeing completeness. The publisher is not liable for damage resulting from a third party’s fraudulent intrusion, nor for the content of third-party sites linked from here, over which it exercises no control.',
    dataTitle: 'Personal data',
    data: (privacy: string, cookies: string) => (
      <>
        Personal data processing is described in the <a href={privacy}>privacy policy</a>, and
        cookie use in the <a href={cookies}>cookie policy</a>.
      </>
    ),
    lawTitle: 'Governing law',
    law: 'This legal notice is governed by French law. Failing an amicable settlement, the French courts have jurisdiction.',
  },
} as const

export function LegalNotice({
  publisher,
  site,
  hosts,
  locale = 'fr',
  updatedAt,
  children,
  classNames,
}: LegalNoticeProps) {
  const t = copy[locale]
  const paths = resolvePaths(site)
  const c = classNames
  const list = c?.list ?? 'list-disc space-y-1 pl-5'
  const link = c?.link ?? 'underline underline-offset-2'

  return (
    <LegalShell title={t.title} updated={formatUpdated(updatedAt, locale)} classNames={c}>
      <Section title={t.publisherTitle} classNames={c}>
        <p>{t.publishedBy(site.domain, publisher.name)}</p>
        <ul className={list}>
          <li>
            {t.legalForm} : {publisher.legalForm}
          </li>
          <li>
            {t.office} : {publisher.address.join(', ')}
          </li>
          <li>
            {t.registration} : {publisher.registration}
          </li>
          {publisher.apeCode ? (
            <li>
              {t.ape} : {publisher.apeCode}
            </li>
          ) : null}
          {publisher.phone ? (
            <li>
              {t.phone} : {publisher.phone}
            </li>
          ) : null}
          <li>
            {t.contact} :{' '}
            <a className={link} href={`mailto:${site.contactEmail}`}>
              {site.contactEmail}
            </a>
          </li>
          <li>
            {t.director} : {publisher.publicationDirector}
          </li>
        </ul>
        {publisher.vatExempt ? <p>{t.vatExempt}</p> : null}
        {publisher.vatNumber ? (
          <p>
            {t.vatNumber} : {publisher.vatNumber}
          </p>
        ) : null}
      </Section>

      <Section title={t.hostTitle} classNames={c}>
        <p>{t.hostIntro}</p>
        <ul className={list}>
          {hosts.map((host) => (
            <li key={host.name}>
              {host.name} — {host.address.join(', ')}
              {host.phone ? ` — ${host.phone}` : ''}
            </li>
          ))}
        </ul>
      </Section>

      {children}

      <Section title={t.ipTitle} classNames={c}>
        <p>{t.ip}</p>
      </Section>

      <Section title={t.liabilityTitle} classNames={c}>
        <p>{t.liability}</p>
      </Section>

      <Section title={t.dataTitle} classNames={c}>
        <p>{t.data(paths.privacy, paths.cookies)}</p>
      </Section>

      <Section title={t.lawTitle} classNames={c}>
        <p>{t.law}</p>
      </Section>
    </LegalShell>
  )
}
