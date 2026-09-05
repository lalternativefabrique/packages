export type Publisher = {
  /** Legal or trade name of the publishing entity, e.g. "L'Alternative Fabrique". */
  name: string
  /** Legal form as it must appear under art. 6-III LCEN, e.g. "entrepreneur individuel". */
  legalForm: string
  /** Registered office, one line per row. */
  address: string[]
  /** SIRET or RCS registration number. Mandatory in a French legal notice. */
  registration: string
  /** NAF/APE code, e.g. "6201Z". */
  apeCode?: string
  /** Natural person answering for published content (art. 6-III LCEN). */
  publicationDirector: string
  /** Omit once the entity leaves the VAT franchise regime. */
  vatExempt?: boolean
  /** Intra-community VAT number, once liable. */
  vatNumber?: string
  /** Published only when a line actually exists: art. 6-III LCEN and, in B2C, art. L. 221-5 C. consom. */
  phone?: string
}

export type Host = {
  name: string
  address: string[]
  phone?: string
  url?: string
}

export type Site = {
  /** Product name as shown to users, e.g. "Techtuel". */
  product: string
  /** Bare domain, e.g. "techtuel.com". */
  domain: string
  contactEmail: string
  /** Route prefixes, so a site can mount these pages wherever it likes. */
  paths?: Partial<LegalPaths>
}

export type LegalPaths = {
  legal: string
  privacy: string
  cookies: string
  terms: string
  aup: string
  dpa: string
}

export const defaultPaths: LegalPaths = {
  legal: '/legal',
  privacy: '/privacy',
  cookies: '/cookies',
  terms: '/cgv',
  aup: '/aup',
  dpa: '/dpa',
}

export type Locale = 'fr' | 'en'

export type LegalContext = {
  publisher: Publisher
  site: Site
  hosts: Host[]
  locale?: Locale
  /** Shown as the "last updated" line. ISO date, formatted per locale. */
  updatedAt: string
}

export function resolvePaths(site: Site): LegalPaths {
  return { ...defaultPaths, ...site.paths }
}
