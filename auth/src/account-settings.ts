export const MIN_PASSWORD_LENGTH = 8

export type PasswordChangeProblem = "missing" | "too_short" | "mismatch" | "unchanged"

export function passwordChangeProblem(input: {
  current: string
  next: string
  confirm: string
}): PasswordChangeProblem | undefined {
  if (!input.current || !input.next) return "missing"
  if (input.next.length < MIN_PASSWORD_LENGTH) return "too_short"
  if (input.next !== input.confirm) return "mismatch"
  if (input.next === input.current) return "unchanged"
  return undefined
}

export type EmailChangeProblem = "invalid" | "unchanged"

export function emailChangeProblem(
  current: string,
  next: string,
): EmailChangeProblem | undefined {
  const normalized = next.trim().toLowerCase()
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized)) return "invalid"
  if (normalized === current.trim().toLowerCase()) return "unchanged"
  return undefined
}

/** Better Auth's listAccounts: a password lives on the "credential" account. */
export function hasPasswordAccount(
  accounts: ReadonlyArray<{ providerId?: string }> | null | undefined,
): boolean | undefined {
  if (!accounts) return undefined
  return accounts.some((a) => a.providerId === "credential")
}
