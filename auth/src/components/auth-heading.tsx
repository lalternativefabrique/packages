interface AuthHeadingProps {
  title: string
  subtitle?: string
  /** Replaces the default size and weight — pass the whole look */
  titleClassName?: string
}

/**
 * The heading of an auth screen, centred above left-aligned fields. That
 * contrast of alignment is what gives the card its hierarchy without a rule.
 *
 * It lives in the form rather than in the layout so a screen carries exactly
 * one <h1>, and so the copy travels with the component that owns the step.
 */
export function AuthHeading({
  title,
  subtitle,
  titleClassName = "text-2xl font-semibold tracking-tight",
}: AuthHeadingProps) {
  return (
    <header className="space-y-1.5 text-center">
      <h1 className={`leading-tight text-foreground ${titleClassName}`}>
        {title}
      </h1>
      {subtitle && (
        <p className="text-balance text-sm leading-relaxed text-muted-foreground">
          {subtitle}
        </p>
      )}
    </header>
  )
}
