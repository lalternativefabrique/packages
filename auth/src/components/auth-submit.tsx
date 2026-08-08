import type { ReactNode } from "react"

interface AuthSubmitProps {
  pending?: boolean
  disabled?: boolean
  pendingLabel: string
  children: ReactNode
}

/**
 * The primary action of an auth screen.
 *
 * The label swaps to its pending form in place, with the spinner absolutely
 * positioned: a spinner inserted into the flow would widen the row and shift
 * the text sideways at the exact moment the person is watching it.
 *
 * The press feedback is a 1px translate rather than a scale — scaling a
 * full-width button visibly blurs its text mid-transform.
 */
export function AuthSubmit({
  pending = false,
  disabled = false,
  pendingLabel,
  children,
}: AuthSubmitProps) {
  return (
    <button
      type="submit"
      disabled={disabled || pending}
      aria-busy={pending || undefined}
      className={[
        "relative flex h-[52px] w-full items-center justify-center rounded-xl sm:h-11",
        "bg-primary text-base font-medium text-primary-foreground sm:text-sm",
        "shadow-sm transition-[background-color,transform,box-shadow] duration-150 ease-out",
        "hover:bg-primary/90 active:translate-y-px",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        "disabled:pointer-events-none disabled:opacity-55",
      ].join(" ")}
    >
      {pending && (
        <span
          aria-hidden="true"
          className="absolute left-4 size-4 animate-spin rounded-full border-2 border-current border-t-transparent opacity-70 motion-reduce:animate-none"
        />
      )}
      {pending ? pendingLabel : children}
    </button>
  )
}
