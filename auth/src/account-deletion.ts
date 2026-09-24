export type AccountDeletionStepId = "billing" | "data"

export type AccountDeletionStepStatus = "done" | "failed" | "pending"

export interface AccountDeletionStep {
  id: AccountDeletionStepId
  status: AccountDeletionStepStatus
}

export interface AccountDeletionReport {
  deleted: boolean
  steps: Array<AccountDeletionStep>
}

/**
 * What a product does to delete one account. Both must be idempotent: the
 * person retries the whole deletion from the start when a step failed, so a
 * step already done runs again and must succeed as a no-op.
 */
export interface AccountDeletionSteps {
  /** Omit on a product that bills nobody; the step is then not shown. */
  cancelBilling?: () => Promise<void>
  deleteData: () => Promise<void>
  onError?: (step: AccountDeletionStepId, error: unknown) => void
}

/**
 * Runs the steps in order and stops at the first failure. Billing goes first:
 * an account deleted but still charged is the one outcome nobody notices.
 */
export async function runAccountDeletion(
  steps: AccountDeletionSteps,
): Promise<AccountDeletionReport> {
  const plan: Array<[AccountDeletionStepId, () => Promise<void>]> = []
  if (steps.cancelBilling) plan.push(["billing", steps.cancelBilling])
  plan.push(["data", steps.deleteData])

  const report: Array<AccountDeletionStep> = plan.map(([id]) => ({
    id,
    status: "pending",
  }))
  for (let i = 0; i < plan.length; i++) {
    try {
      await plan[i][1]()
      report[i].status = "done"
    } catch (error) {
      report[i].status = "failed"
      steps.onError?.(plan[i][0], error)
      return { deleted: false, steps: report }
    }
  }
  return { deleted: true, steps: report }
}
