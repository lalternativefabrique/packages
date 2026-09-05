import { LegalShell, Section, formatUpdated, type LegalClassNames } from './shell'
import { resolvePaths, type LegalContext } from './types'

export type CookieEntry = {
  name: string
  purpose: string
  /** Left free-form: "session", "12 mois", "13 months". */
  retention: string
}

export type CookiesPageProps = LegalContext & {
  /** The cookies this product actually sets. Wrong here means a false declaration. */
  cookies: CookieEntry[]
  /**
   * Whether every listed cookie is strictly necessary to deliver the service the
   * user asked for. When false the consent exemption collapses and the page says
   * so instead of claiming a banner is unnecessary.
   */
  strictlyNecessaryOnly: boolean
  classNames?: LegalClassNames
}

const copy = {
  fr: {
    title: 'Politique cookies',
    whatWeSet: 'Ce que nous déposons',
    intro: (product: string, domain: string) =>
      `${product} dépose les cookies suivants sur le domaine ${domain} et ses sous-domaines :`,
    colName: 'Cookie',
    colPurpose: 'Finalité',
    colRetention: 'Durée',
    whatWeDont: 'Ce que nous ne déposons pas',
    noneIntro: 'Nous ne déposons, sur aucune de nos pages :',
    none: [
      'aucun cookie publicitaire ou de reciblage ;',
      'aucun traceur tiers, ni réseau social, ni régie ;',
      "aucun outil de mesure d'audience déposant un identifiant de suivi ;",
      'aucun cookie de profilage, de scoring ou de partage de données avec un tiers.',
    ],
    noBannerTitle: "Pourquoi il n'y a pas de bandeau de consentement",
    noBanner: [
      "Le consentement préalable de l'utilisateur est requis pour les cookies qui ne sont pas strictement nécessaires à la fourniture d'un service expressément demandé — typiquement les cookies publicitaires ou de mesure d'audience à des fins autres que purement techniques.",
      "Les cookies listés ci-dessus entrent tous dans la catégorie strictement nécessaire : maintien de la session, sécurité de l'authentification, préférences d'affichage. Ils sont donc dispensés de consentement préalable, et un bandeau de consentement n'aurait rien à vous demander.",
      "Cette dispense cesserait immédiatement si nous introduisions un traceur non nécessaire. Dans ce cas, un mécanisme de recueil du consentement serait mis en place avant tout dépôt, et la présente page serait mise à jour en conséquence.",
    ],
    bannerTitle: 'Consentement',
    banner:
      "Certains des cookies listés ci-dessus ne sont pas strictement nécessaires au service. Ils ne sont déposés qu'après votre consentement, que vous pouvez retirer à tout moment.",
    manageTitle: 'Gérer les cookies',
    manage:
      "Vous pouvez à tout moment supprimer les cookies déposés ou les bloquer depuis les paramètres de votre navigateur. Le blocage des cookies de session empêchera toutefois la connexion à votre compte : le service n'a pas d'autre moyen de reconnaître une session authentifiée.",
    contactTitle: 'Contact',
    contact: (email: string) =>
      `Pour toute question relative à cette politique ou au traitement de vos données, écrivez à ${email}. Vous pouvez également consulter les recommandations de la CNIL en matière de cookies sur cnil.fr.`,
    relatedTitle: 'Documents liés',
    related: {
      legal: 'Mentions légales',
      privacy: 'Politique de confidentialité',
      terms: "Conditions générales de vente et d'utilisation",
    },
  },
  en: {
    title: 'Cookie policy',
    whatWeSet: 'What we set',
    intro: (product: string, domain: string) =>
      `${product} sets the following cookies on ${domain} and its subdomains:`,
    colName: 'Cookie',
    colPurpose: 'Purpose',
    colRetention: 'Retention',
    whatWeDont: 'What we do not set',
    noneIntro: 'On none of our pages do we set:',
    none: [
      'any advertising or retargeting cookie;',
      'any third-party tracker, social network or ad network;',
      'any analytics tool setting a tracking identifier;',
      'any profiling or scoring cookie, or any cookie sharing data with a third party.',
    ],
    noBannerTitle: 'Why there is no consent banner',
    noBanner: [
      'Prior consent is required for cookies that are not strictly necessary to deliver a service the user explicitly requested — typically advertising cookies, or analytics used for anything beyond purely technical purposes.',
      'Every cookie listed above is strictly necessary: keeping you signed in, securing authentication, remembering display preferences. They are therefore exempt from prior consent, and a banner would have nothing to ask you.',
      'That exemption would end the moment we introduced a non-necessary tracker. Consent would then be collected before any such cookie is set, and this page updated accordingly.',
    ],
    bannerTitle: 'Consent',
    banner:
      'Some cookies listed above are not strictly necessary. They are set only after you consent, and you may withdraw that consent at any time.',
    manageTitle: 'Managing cookies',
    manage:
      'You can delete or block cookies at any time from your browser settings. Blocking session cookies will prevent you from signing in: the service has no other way to recognise an authenticated session.',
    contactTitle: 'Contact',
    contact: (email: string) =>
      `For any question about this policy or how we process your data, write to ${email}.`,
    relatedTitle: 'Related documents',
    related: {
      legal: 'Legal notice',
      privacy: 'Privacy policy',
      terms: 'Terms of sale and use',
    },
  },
} as const

export function CookiesPage({
  publisher: _publisher,
  site,
  locale = 'fr',
  updatedAt,
  cookies,
  strictlyNecessaryOnly,
  classNames,
}: CookiesPageProps) {
  const t = copy[locale]
  const paths = resolvePaths(site)
  const c = classNames

  return (
    <LegalShell title={t.title} updated={formatUpdated(updatedAt, locale)} classNames={c}>
      <Section title={t.whatWeSet} classNames={c}>
        <p>{t.intro(site.product, site.domain)}</p>
        <ul className={c?.list ?? 'list-disc space-y-2 pl-5'}>
          {cookies.map((cookie) => (
            <li key={cookie.name}>
              <code>{cookie.name}</code> — {cookie.purpose} ({cookie.retention})
            </li>
          ))}
        </ul>
      </Section>

      <Section title={t.whatWeDont} classNames={c}>
        <p>{t.noneIntro}</p>
        <ul className={c?.list ?? 'list-disc space-y-1 pl-5'}>
          {t.none.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ul>
      </Section>

      {strictlyNecessaryOnly ? (
        <Section title={t.noBannerTitle} classNames={c}>
          {t.noBanner.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </Section>
      ) : (
        <Section title={t.bannerTitle} classNames={c}>
          <p>{t.banner}</p>
        </Section>
      )}

      <Section title={t.manageTitle} classNames={c}>
        <p>{t.manage}</p>
      </Section>

      <Section title={t.contactTitle} classNames={c}>
        <p>{t.contact(site.contactEmail)}</p>
      </Section>

      <Section title={t.relatedTitle} classNames={c}>
        <ul className={c?.list ?? 'list-disc space-y-1 pl-5'}>
          <li>
            <a className={c?.link ?? 'underline underline-offset-2'} href={paths.legal}>
              {t.related.legal}
            </a>
          </li>
          <li>
            <a className={c?.link ?? 'underline underline-offset-2'} href={paths.privacy}>
              {t.related.privacy}
            </a>
          </li>
          <li>
            <a className={c?.link ?? 'underline underline-offset-2'} href={paths.terms}>
              {t.related.terms}
            </a>
          </li>
        </ul>
      </Section>
    </LegalShell>
  )
}
