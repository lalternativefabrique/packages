import type { ReactNode } from "react"

export interface AdminKpiProps {
  label: string
  /** The figure itself. Pass a string so the caller owns the formatting. */
  value: ReactNode
  hint?: string
  loading?: boolean
}

/**
 * One metric tile: a big number over a quiet label and hint.
 *
 * The loading state is a skeleton of the figure's own size rather than an
 * ellipsis, so the tile does not resize when the number lands.
 */
export function AdminKpi({ label, value, hint, loading = false }: AdminKpiProps) {
  return (
    <div className="admin-paper rounded-xl p-5">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      {loading ? (
        <span
          aria-hidden
          className="mt-2 block h-9 w-16 animate-pulse rounded bg-muted"
        />
      ) : (
        <p className="mt-2 text-3xl font-semibold tabular-nums leading-none">
          {value}
        </p>
      )}
      {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
    </div>
  )
}
