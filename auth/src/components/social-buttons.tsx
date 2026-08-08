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
      <div className="relative">
        <div className="absolute inset-0 flex items-center">
          <div className="w-full border-t border-input" />
        </div>
        <div className="relative flex justify-center">
          <span className="bg-background px-2 text-xs uppercase tracking-wider text-muted-foreground">
            {separator}
          </span>
        </div>
      </div>

      <div className="space-y-2">
        {providers.map((provider) => (
          <button
            key={provider}
            type="button"
            onClick={() => onSelect(provider)}
            disabled={disabled}
            className="inline-flex h-11 w-full items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
          >
            {copy[provider] ?? provider}
          </button>
        ))}
      </div>
    </div>
  )
}
