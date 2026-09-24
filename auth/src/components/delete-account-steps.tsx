import { useState } from "react"
import type {
  AccountDeletionReport,
  AccountDeletionStepId,
  AccountDeletionStepStatus,
} from "../account-deletion"
import { AuthAlert } from "./auth-alert"

export interface DeleteAccountStepsLabels {
  warning: string
  start: string
  confirm: string
  cancel: string
  running: string
  retry: string
  failed: string
  unreachable: string
  steps: Record<AccountDeletionStepId, string>
}

export interface DeleteAccountStepsProps {
  /** The route answering an {@link AccountDeletionReport}. */
  endpoint?: string
  /** Whether the product bills; decides if the billing step is announced. */
  billing?: boolean
  onDeleted?: () => void | Promise<void>
  labels?: Partial<DeleteAccountStepsLabels>
}

const DEFAULTS: DeleteAccountStepsLabels = {
  warning:
    "La suppression est définitive : ton abonnement est résilié puis toutes tes données sont effacées.",
  start: "Supprimer mon compte",
  confirm: "Oui, supprimer définitivement",
  cancel: "Annuler",
  running: "Suppression en cours…",
  retry: "Réessayer",
  failed:
    "La suppression n'a pas pu aller au bout. Les étapes validées sont conservées : relance pour terminer.",
  unreachable: "Le service est injoignable. Réessaie dans un instant.",
  steps: {
    billing: "Résiliation de votre abonnement",
    data: "Suppression de vos données",
  },
}

const MARK: Record<AccountDeletionStepStatus | "running", string> = {
  done: "✓",
  failed: "✕",
  pending: "○",
  running: "…",
}

type Phase = "idle" | "confirming" | "running" | "failed" | "deleted"

export function DeleteAccountSteps({
  endpoint = "/api/auth/delete-account",
  billing = false,
  onDeleted,
  labels,
}: DeleteAccountStepsProps) {
  const t = {
    ...DEFAULTS,
    ...labels,
    steps: { ...DEFAULTS.steps, ...labels?.steps },
  }
  const initial: AccountDeletionReport["steps"] = [
    ...(billing ? [{ id: "billing" as const, status: "pending" as const }] : []),
    { id: "data", status: "pending" },
  ]
  const [phase, setPhase] = useState<Phase>("idle")
  const [steps, setSteps] = useState(initial)
  const [error, setError] = useState<string | undefined>()

  const run = async () => {
    setPhase("running")
    setError(undefined)
    try {
      const res = await fetch(endpoint, {
        method: "POST",
        credentials: "include",
      })
      const report = (await res.json().catch(() => null)) as
        | AccountDeletionReport
        | null
      if (!report?.steps) {
        setPhase("failed")
        setError(t.unreachable)
        return
      }
      setSteps(report.steps)
      if (report.deleted) {
        setPhase("deleted")
        await onDeleted?.()
        return
      }
      setPhase("failed")
      setError(t.failed)
    } catch {
      setPhase("failed")
      setError(t.unreachable)
    }
  }

  if (phase === "idle") {
    return (
      <button
        type="button"
        onClick={() => setPhase("confirming")}
        className="rounded-md border border-destructive/40 px-4 py-2 text-sm font-medium text-destructive hover:bg-destructive/10"
      >
        {t.start}
      </button>
    )
  }

  if (phase === "confirming") {
    return (
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{t.warning}</p>
        <div className="flex gap-3">
          <button
            type="button"
            onClick={run}
            className="rounded-md bg-destructive px-4 py-2 text-sm font-medium text-white hover:bg-destructive/90"
          >
            {t.confirm}
          </button>
          <button
            type="button"
            onClick={() => setPhase("idle")}
            className="rounded-md px-4 py-2 text-sm text-muted-foreground hover:bg-muted"
          >
            {t.cancel}
          </button>
        </div>
      </div>
    )
  }

  const running = phase === "running"
  const current = steps.findIndex((s) => s.status !== "done")

  return (
    <div className="space-y-4">
      <ol className="space-y-2" aria-live="polite">
        {steps.map((step, i) => {
          const status =
            running && i === current ? "running" : step.status
          return (
            <li key={step.id} className="flex items-center gap-3 text-sm">
              <span
                aria-hidden="true"
                className={
                  status === "done"
                    ? "text-emerald-600"
                    : status === "failed"
                      ? "text-destructive"
                      : "text-muted-foreground"
                }
              >
                {MARK[status]}
              </span>
              <span>{t.steps[step.id]}</span>
            </li>
          )
        })}
      </ol>
      <AuthAlert>{error}</AuthAlert>
      {running ? (
        <p className="text-sm text-muted-foreground">{t.running}</p>
      ) : null}
      {phase === "failed" ? (
        <button
          type="button"
          onClick={run}
          className="rounded-md bg-destructive px-4 py-2 text-sm font-medium text-white hover:bg-destructive/90"
        >
          {t.retry}
        </button>
      ) : null}
    </div>
  )
}
