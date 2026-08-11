import type { ReactNode } from "react"

interface BrandPanelProps {
  title: string
  tagline?: string
  children?: ReactNode
}

/**
 * What an app can put in AuthLayout's `panel` slot without owning a single
 * asset: a colour field carrying a line of copy.
 *
 * The gradient is two stops of one hue rather than a photograph, so it costs
 * no request, scales to any viewport, and cannot arrive half-loaded behind a
 * form someone is already typing into.
 */
export function BrandPanel({ title, tagline, children }: BrandPanelProps) {
  return (
    <div className="relative flex h-full w-full flex-col justify-end overflow-hidden bg-gradient-to-br from-amber-400 via-amber-500 to-orange-600 p-9">
      <div
        aria-hidden="true"
        className="absolute -right-16 -top-16 size-64 rounded-full bg-white/15 blur-2xl"
      />
      <div
        aria-hidden="true"
        className="absolute -bottom-24 -left-12 size-72 rounded-full bg-orange-700/25 blur-3xl"
      />

      <div className="relative space-y-3">
        <p className="text-[1.75rem] font-semibold leading-[1.15] tracking-[-0.02em] text-white">
          {title}
        </p>
        {tagline && (
          <p className="max-w-[24ch] text-pretty text-sm leading-relaxed text-white/75">
            {tagline}
          </p>
        )}
        {children}
      </div>
    </div>
  )
}
