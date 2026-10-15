export const SECOND_FACTOR_REQUIRED = "second_factor_required"

type Failure = { code?: string; message?: string } | null | undefined

export function isSecondFactorRequired(error: Failure): boolean {
  return error?.code === SECOND_FACTOR_REQUIRED
}

export type SecondFactorOutcome =
  | { kind: "invalid" }
  | { kind: "expired" }
  | { kind: "refused"; message?: string }

export function secondFactorOutcome(error: Failure): SecondFactorOutcome {
  if (error?.code === "invalid_code") return { kind: "invalid" }
  if (error?.code === "flow_expired") return { kind: "expired" }
  return { kind: "refused", message: error?.message }
}
