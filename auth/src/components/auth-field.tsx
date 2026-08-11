import { useId, useState, type InputHTMLAttributes, type ReactNode } from "react"

type NativeProps = Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "className">

export interface AuthFieldProps extends NativeProps {
  label: string
  /** Rendered on the right of the label row — typically a "forgot password" link */
  hint?: ReactNode
  /** Persistent helper text under the field, tied to it for screen readers */
  description?: string
  invalid?: boolean
  /** Replaces the default border and background utilities */
  fieldClassName?: string
}

/**
 * A single credential field.
 *
 * The label is a real <label>, always visible, never a placeholder standing in
 * for one: a placeholder disappears the moment someone types, which is exactly
 * when a person double-checking a long password needs to know what the box is.
 *
 * Touch targets are 52px tall on mobile and 44px from sm up. Below ~48px,
 * thumbs miss; on a pointer device the same height reads as oversized, so the
 * two are not one compromise value.
 *
 * The focus ring is a ring rather than an outline so it follows the rounded
 * corners exactly, and the border darkens at the same time — the border is what
 * survives forced-colors mode, where the ring is dropped.
 */
export function AuthField({
  label,
  hint,
  description,
  invalid = false,
  fieldClassName = "border-foreground/30 bg-background",
  ...props
}: AuthFieldProps) {
  const id = useId()
  const descriptionId = description ? `${id}-description` : undefined
  const [revealed, setRevealed] = useState(false)

  const isPassword = props.type === "password"
  const type = isPassword && revealed ? "text" : props.type

  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        {/* Small caps with wide tracking: at this size the label reads as a
            field marker rather than as prose competing with the heading. */}
        <label
          htmlFor={id}
          className="text-[0.6875rem] font-semibold uppercase leading-none tracking-[0.09em] text-foreground/70"
        >
          {label}
        </label>
        {hint}
      </div>

      <div className="relative">
        <input
          {...props}
          id={id}
          type={type}
          aria-invalid={invalid || undefined}
          aria-describedby={descriptionId}
          className={[
            "h-[52px] w-full rounded-md border px-3.5 text-base",
            "sm:h-[46px] sm:text-[0.9375rem]",
            isPassword ? "pr-12" : "",
            "text-foreground placeholder:text-muted-foreground/60",
            // Focus reads as the border committing rather than as a halo
            // appearing beside it: the ring is tight and the border darkens to
            // full ink in the same 180ms, so the field answers the caret.
            "transition-[border-color,box-shadow] duration-200 ease-out",
            "focus-visible:outline-none focus-visible:ring-2",
            invalid
              ? "border-destructive bg-background focus-visible:border-destructive focus-visible:ring-destructive/15"
              // The default border is foreground/30, not border-input: the
              // latter against a white card lands near 1.3:1, well under the
              // 3:1 WCAG 1.4.11 asks of a control's boundary — the field reads
              // as a faint tint rather than as something to type in.
              : `${fieldClassName} focus-visible:border-foreground focus-visible:ring-foreground/10`,
            "disabled:cursor-not-allowed disabled:opacity-60",
          ]
            .filter(Boolean)
            .join(" ")}
        />

        {isPassword && (
          <button
            type="button"
            onClick={() => setRevealed((v) => !v)}
            disabled={props.disabled}
            aria-pressed={revealed}
            aria-label={
              revealed ? "Masquer le mot de passe" : "Afficher le mot de passe"
            }
            className="absolute inset-y-0 right-0 flex w-12 items-center justify-center rounded-r-xl text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-60"
          >
            <EyeIcon open={revealed} />
          </button>
        )}
      </div>

      {description && (
        <p id={descriptionId} className="text-xs text-muted-foreground">
          {description}
        </p>
      )}
    </div>
  )
}

// Inline rather than from lucide-react: the package would gain a dependency
// consumers already have at a different version, for two paths.
function EyeIcon({ open }: { open: boolean }) {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0Z" />
      <circle cx="12" cy="12" r="3" />
      {!open && <path d="m3 3 18 18" />}
    </svg>
  )
}
