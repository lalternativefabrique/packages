const LABELS: Record<string, string> = {
  google: "Continuer avec Google",
  github: "Continuer avec GitHub",
}

interface SocialButtonsProps {
  providers: Array<"google" | "github">
  onSelect: (provider: "google" | "github") => void | Promise<void>
  disabled?: boolean
  /** Divider text between the password form and the providers */
  separator?: string
  /** Per-provider button copy, merged over the French defaults */
  labels?: Partial<Record<"google" | "github", string>>
}

export function SocialButtons({
  providers,
  onSelect,
  disabled = false,
  separator = "ou",
  labels,
}: SocialButtonsProps) {
  if (providers.length === 0) return null

  const copy = { ...LABELS, ...labels }

  return (
    <div className="space-y-4">
      {/* The rule is two flex segments rather than a line behind an opaque
          label: an opaque background only hides the rule when it matches the
          surface behind it, which breaks the moment this sits on a card. */}
      <div aria-hidden="true" className="flex items-center gap-3">
        <span className="h-px flex-1 bg-border" />
        <span className="text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
          {separator}
        </span>
        <span className="h-px flex-1 bg-border" />
      </div>

      <div className="space-y-2.5">
        {providers.map((provider) => (
          <button
            key={provider}
            type="button"
            onClick={() => onSelect(provider)}
            disabled={disabled}
            className={[
              "flex h-[52px] w-full items-center justify-center gap-2.5 rounded-xl sm:h-11",
              "border border-input bg-background text-base font-medium text-foreground sm:text-sm",
              "transition-[background-color,border-color,transform] duration-150 ease-out",
              "hover:border-foreground/20 hover:bg-accent active:translate-y-px",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
              "disabled:pointer-events-none disabled:opacity-55",
            ].join(" ")}
          >
            <ProviderMark provider={provider} />
            {copy[provider] ?? provider}
          </button>
        ))}
      </div>
    </div>
  )
}

// Brand marks are inlined: they must keep their own colors (Google's mark is
// unusable in monochrome) and adding an icon dependency to this package would
// duplicate one every consumer already ships.
function ProviderMark({ provider }: { provider: "google" | "github" }) {
  if (provider === "google") {
    return (
      <svg width="17" height="17" viewBox="0 0 18 18" aria-hidden="true">
        <path
          fill="#4285F4"
          d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
        />
        <path
          fill="#34A853"
          d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.02-3.7H.96v2.33A9 9 0 0 0 9 18Z"
        />
        <path
          fill="#FBBC05"
          d="M3.98 10.72a5.4 5.4 0 0 1 0-3.44V4.95H.96a9 9 0 0 0 0 8.1l3.02-2.33Z"
        />
        <path
          fill="#EA4335"
          d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58C13.46.9 11.43 0 9 0A9 9 0 0 0 .96 4.95l3.02 2.33C4.68 5.16 6.66 3.58 9 3.58Z"
        />
      </svg>
    )
  }
  return (
    <svg width="17" height="17" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      <path d="M8 0a8 8 0 0 0-2.53 15.59c.4.07.55-.17.55-.38l-.01-1.49c-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.4 7.4 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48l-.01 2.2c0 .21.15.46.55.38A8 8 0 0 0 8 0Z" />
    </svg>
  )
}
