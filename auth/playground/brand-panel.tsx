import type { ReactNode } from "react"

interface BrandPanelProps {
  title: string
  tagline?: string
  /** Tailwind gradient utilities — the app's own colour */
  gradient: string
  /** Ink for the copy: a light field needs dark type, not white */
  tone?: "light" | "dark"
  children?: ReactNode
}

/**
 * What an app can put in AuthLayout's `panel` slot without owning a single
 * asset: a colour field carrying a line of copy.
 *
 * The gradient is stops of one hue rather than a photograph, so it costs no
 * request, scales to any viewport, and cannot arrive half-loaded behind a form
 * someone is already typing into.
 */
export function BrandPanel({
  title,
  tagline,
  gradient,
  tone = "light",
  children,
}: BrandPanelProps) {
  const ink = tone === "dark" ? "text-zinc-900" : "text-white"
  const inkMuted = tone === "dark" ? "text-zinc-900/70" : "text-white/75"
  return (
    <div
      className={`relative flex h-full w-full flex-col justify-end overflow-hidden bg-gradient-to-br md:p-9 ${gradient}`}
    >
      <div
        aria-hidden="true"
        className="absolute -right-16 -top-16 size-64 rounded-full bg-white/15 blur-2xl"
      />
      <div
        aria-hidden="true"
        className="absolute -bottom-24 -left-12 size-72 rounded-full bg-black/15 blur-3xl"
      />

      <div className="relative hidden space-y-3 md:block">
        <p className={`text-[1.75rem] font-semibold leading-[1.15] tracking-[-0.02em] ${ink}`}>
          {title}
        </p>
        {tagline && (
          <p className={`max-w-[24ch] text-pretty text-sm leading-relaxed ${inkMuted}`}>
            {tagline}
          </p>
        )}
        {children}
      </div>
    </div>
  )
}
