const LABELS: Record<string, string> = {
  google: "Continue with Google",
  github: "Continue with GitHub",
}

interface SocialButtonsProps {
  providers: Array<"google" | "github">
  onSelect: (provider: "google" | "github") => void | Promise<void>
  disabled?: boolean
}

export function SocialButtons({
  providers,
  onSelect,
  disabled = false,
}: SocialButtonsProps) {
  if (providers.length === 0) return null

  return (
    <div className="space-y-4">
      <div className="relative">
        <div className="absolute inset-0 flex items-center">
          <div className="w-full border-t border-input" />
        </div>
        <div className="relative flex justify-center">
          <span className="bg-background px-2 text-xs uppercase tracking-wider text-muted-foreground">
            or
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
            {LABELS[provider] ?? provider}
          </button>
        ))}
      </div>
    </div>
  )
}
