import type { ReactNode } from "react"

interface AuthSubmitProps {
  pending?: boolean
  disabled?: boolean
  pendingLabel: string
  children: ReactNode
  /** Replaces the default `bg-primary text-primary-foreground hover:bg-primary/90` */
  className?: string
  /**
   * Adds the step that detaches the action from the fields above it. The
   * inputs sit on a tighter rhythm so they read as one block to fill in; the
   * button is what you do once that block is done, and an even gap would put
   * it on the same footing as another field.
   */
  spacedAbove?: boolean
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
  className = "bg-primary text-primary-foreground hover:bg-primary/90",
  spacedAbove = false,
}: AuthSubmitProps) {
  return (
    <button
      type="submit"
      disabled={disabled || pending}
      aria-busy={pending || undefined}
      className={[
        spacedAbove ? "!mt-7" : "",
        "relative flex h-[52px] w-full items-center justify-center rounded-md sm:h-[46px]",
        // Slightly tracked at this weight: a wide solid button set in plain
        // medium reads as a slab, and the letterspacing is what makes it read
        // as typeset rather than filled in.
        "text-[0.9375rem] font-medium tracking-[0.01em]",
        className,
        "transition-[background-color,transform,box-shadow] duration-200 ease-out",
        "shadow-[0_1px_2px_rgba(0,0,0,0.08)] hover:shadow-[0_2px_8px_rgba(0,0,0,0.12)]",
        "active:translate-y-px active:shadow-[0_1px_2px_rgba(0,0,0,0.08)]",
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
