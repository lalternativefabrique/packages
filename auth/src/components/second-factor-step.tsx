import { useState, type FormEvent } from "react"
import type { AuthClientResult, SecondFactorStepLabels } from "../types"
import { secondFactorOutcome } from "../second-factor-sign-in"
import { AuthAlert } from "./auth-alert"
import { AuthField } from "./auth-field"
import { AuthSubmit } from "./auth-submit"
import { AUTH_LINK_CLASS } from "./auth-link"

export const SECOND_FACTOR_DEFAULTS: Required<SecondFactorStepLabels> = {
  secondFactorLabel: "Code de vérification",
  secondFactorHint:
    "Les 6 chiffres de ton application d'authentification, ou un code de secours.",
  secondFactorSubmit: "Valider",
  secondFactorPending: "Vérification…",
  secondFactorBack: "Retour",
  secondFactorInvalid: "Code incorrect.",
  secondFactorExpired: "La vérification a expiré, reconnecte-toi.",
}

interface SecondFactorStepProps {
  signIn: (input: { code: string }) => Promise<AuthClientResult>
  onSignedIn: () => Promise<void> | void
  onRestart: (message?: string) => void
  refusalMessage: (message: string | undefined) => string
  labels: Required<SecondFactorStepLabels>
  submitClassName?: string
  fieldClassName?: string
}

export function SecondFactorStep({
  signIn,
  onSignedIn,
  onRestart,
  refusalMessage,
  labels: t,
  submitClassName,
  fieldClassName,
}: SecondFactorStepProps) {
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | undefined>()
  const [pending, setPending] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!code.trim()) return
    setError(undefined)
    setPending(true)
    try {
      const res = await signIn({ code: code.trim() })
      if (!res?.error) {
        await onSignedIn()
        return
      }
      const outcome = secondFactorOutcome(res.error)
      if (outcome.kind === "expired") {
        onRestart(t.secondFactorExpired)
        return
      }
      setError(
        outcome.kind === "invalid"
          ? t.secondFactorInvalid
          : refusalMessage(outcome.message),
      )
    } catch (err) {
      setError(refusalMessage(err instanceof Error ? err.message : undefined))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-7">
      <AuthAlert>{error}</AuthAlert>
      <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
        <AuthField
          label={t.secondFactorLabel}
          type="text"
          inputMode="text"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          required
          autoFocus
          disabled={pending}
          autoComplete="one-time-code"
          autoCapitalize="none"
          spellCheck={false}
          invalid={!!error}
          fieldClassName={fieldClassName}
          description={t.secondFactorHint}
        />
        <AuthSubmit
          spacedAbove
          pending={pending}
          disabled={!code.trim()}
          pendingLabel={t.secondFactorPending}
          className={submitClassName}
        >
          {t.secondFactorSubmit}
        </AuthSubmit>
      </form>
      <p className="text-center text-sm text-muted-foreground">
        <button
          type="button"
          onClick={() => onRestart()}
          disabled={pending}
          className={AUTH_LINK_CLASS}
        >
          {t.secondFactorBack}
        </button>
      </p>
    </div>
  )
}
