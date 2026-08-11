interface AuthOtpFieldProps {
  label: string
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  invalid?: boolean
  id: string
}

export const OTP_LENGTH = 6

/**
 * The 6-digit code field.
 *
 * Not an AuthField: the value is a code being read off another screen, so it
 * is spaced and monospaced to be checked digit by digit, and non-digits are
 * dropped on the way in — pasting a code from a mail client routinely carries
 * a trailing space.
 */
export function AuthOtpField({
  label,
  value,
  onChange,
  disabled = false,
  invalid = false,
  id,
}: AuthOtpFieldProps) {
  return (
    <div className="space-y-2">
      <label
        htmlFor={id}
        className="block text-sm font-medium leading-none text-foreground"
      >
        {label}
      </label>
      <input
        id={id}
        type="text"
        inputMode="numeric"
        pattern="[0-9]*"
        maxLength={OTP_LENGTH}
        value={value}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, ""))}
        required
        disabled={disabled}
        autoComplete="one-time-code"
        aria-invalid={invalid || undefined}
        className={[
          "h-[52px] w-full rounded-xl border bg-background px-4 sm:h-11",
          "text-center font-mono text-lg tracking-[0.4em]",
          "text-foreground",
          "transition-[border-color,box-shadow] duration-150 ease-out",
          "focus-visible:outline-none focus-visible:ring-[3px]",
          invalid
            ? "border-destructive focus-visible:border-destructive focus-visible:ring-destructive/20"
            : "border-input focus-visible:border-ring focus-visible:ring-ring/15",
          "disabled:cursor-not-allowed disabled:opacity-60",
        ].join(" ")}
      />
    </div>
  )
}
