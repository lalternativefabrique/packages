import { messagSignInUrl } from "../messag-sign-in"

export interface MessagSignInProps {
  /** Where the person lands once signed in */
  callbackURL?: string
  label?: string
  disabled?: boolean
  className?: string
}

export function MessagSignIn({
  callbackURL,
  label = "Se connecter avec messag",
  disabled = false,
  className,
}: MessagSignInProps) {
  return (
    <a
      href={disabled ? undefined : messagSignInUrl(callbackURL)}
      aria-disabled={disabled || undefined}
      className={
        className ??
        "flex h-10 w-full items-center justify-center gap-2 rounded-md border border-input bg-background px-4 text-sm font-medium transition-colors hover:bg-accent aria-disabled:pointer-events-none aria-disabled:opacity-50"
      }
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        className="size-4"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <path d="m3 7 9 6 9-6" />
      </svg>
      {label}
    </a>
  )
}
