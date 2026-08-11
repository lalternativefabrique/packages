interface AuthHeadingProps {
  title: string
  subtitle?: string
  /** Replaces the default size and weight — pass the whole look */
  titleClassName?: string
}

/**
 * The heading of an auth screen.
 *
 * This screen is the only one someone sees before they have any reason to
 * trust the product, so the type carries it: the title is the screen's centre
 * of gravity, set large and tightly tracked, with the subtitle stepping well
 * back rather than competing.
 *
 * Optical sizing matters at this weight — `text-balance` keeps a two-line
 * subtitle from breaking into a lonely last word, which reads as an accident
 * where everything else is deliberate.
 */
export function AuthHeading({
  title,
  subtitle,
  titleClassName = "text-[2rem] font-semibold leading-[1.1] tracking-[-0.03em] sm:text-[2.25rem]",
}: AuthHeadingProps) {
  return (
    <header className="space-y-2.5 text-center">
      <h1 className={`text-foreground ${titleClassName}`}>{title}</h1>
      {subtitle && (
        <p className="text-balance text-[0.9375rem] leading-relaxed text-muted-foreground">
          {subtitle}
        </p>
      )}
    </header>
  )
}
