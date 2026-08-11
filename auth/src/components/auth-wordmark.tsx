import type { ReactNode } from "react"

interface AuthWordmarkProps {
  children: ReactNode
  /**
   * A mark set before the name — a dot, a glyph, an app's own icon. Given a
   * colour by the app (`className="text-amber-500"` on an element of its own),
   * it is the one spot of brand on an otherwise neutral screen.
   */
  mark?: ReactNode
  className?: string
}

/**
 * The app's name above an auth card.
 *
 * A name set in the body face at body size reads as a line of text someone
 * forgot to delete. Wide-tracked small caps read as a mark: the letterforms
 * stop being a word to scan and become a shape to recognise, which is what the
 * top of this screen is for.
 *
 * This is the default for apps without a drawn logo. Any app with a real one
 * passes it to `AuthLayout`'s `logo` slot instead — that prop takes any node.
 */
export function AuthWordmark({
  children,
  mark,
  className = "",
}: AuthWordmarkProps) {
  return (
    <div
      className={`flex items-center justify-center gap-2 text-[0.6875rem] font-semibold uppercase tracking-[0.22em] text-foreground ${className}`}
    >
      {mark}
      <span className="-mr-[0.22em]">{children}</span>
    </div>
  )
}
