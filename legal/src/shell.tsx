import type { ReactNode } from 'react'

export type LegalClassNames = {
  root?: string
  title?: string
  updated?: string
  section?: string
  sectionTitle?: string
  sectionBody?: string
  link?: string
  list?: string
}

// Every consuming site brings its own palette (Techtuel keys on `text-ink`,
// Spore on `text-foreground`), so the kit ships structure and lets the caller
// name the classes rather than shipping a look that would be wrong everywhere.
export const defaultClassNames: LegalClassNames = {
  root: 'mx-auto max-w-3xl px-6 py-16',
  title: 'text-3xl font-bold',
  updated: 'mt-3 text-sm opacity-70',
  section: 'mt-10',
  sectionTitle: 'text-lg font-semibold',
  sectionBody: 'mt-3 space-y-3 text-sm leading-relaxed',
  link: 'underline underline-offset-2',
  list: 'list-disc space-y-1 pl-5',
}

export type ShellProps = {
  title: string
  updated: string
  children: ReactNode
  classNames?: LegalClassNames
}

export function LegalShell({ title, updated, children, classNames }: ShellProps) {
  const c = { ...defaultClassNames, ...classNames }
  return (
    <div className={c.root}>
      <h1 className={c.title}>{title}</h1>
      <p className={c.updated}>{updated}</p>
      {children}
    </div>
  )
}

export function Section({
  title,
  children,
  classNames,
}: {
  title: string
  children: ReactNode
  classNames?: LegalClassNames
}) {
  const c = { ...defaultClassNames, ...classNames }
  return (
    <section className={c.section}>
      <h2 className={c.sectionTitle}>{title}</h2>
      <div className={c.sectionBody}>{children}</div>
    </section>
  )
}

export function formatUpdated(iso: string, locale: 'fr' | 'en'): string {
  const date = new Date(`${iso}T00:00:00Z`)
  const formatted = new Intl.DateTimeFormat(locale === 'fr' ? 'fr-FR' : 'en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  }).format(date)
  return locale === 'fr' ? `Dernière mise à jour : ${formatted}` : `Last updated: ${formatted}`
}
