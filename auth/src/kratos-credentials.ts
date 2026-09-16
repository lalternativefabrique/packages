/**
 * The value written in place of a password hash once Kratos holds the
 * password. Better Auth's sign-in route refuses the request before reaching
 * the verifier when the credential account carries no hash, so a row must
 * exist and be non-empty for the Kratos check to run at all.
 *
 * It is not a hash and cannot become one: argon2/bcrypt/scrypt verification
 * of this string fails on its format, so a build that ever bypassed the
 * custom verifier refuses everyone instead of admitting anyone.
 */
export const KRATOS_SENTINEL_HASH = "kratos:external-credential:no-local-hash"

export function isKratosSentinel(hash: string | null | undefined): boolean {
  return hash === KRATOS_SENTINEL_HASH
}

export type KratosOutcome =
  | { status: "valid"; identityId: string }
  | { status: "invalid_credentials" }
  | { status: "no_credential" }
  | { status: "email_not_verified" }
  | { status: "second_factor_required" }
  | { status: "account_disabled" }
  | { status: "unavailable" }

export interface KratosCredentialCheck {
  email: string
  password: string
}

interface KratosLoginFlow {
  id: string
  ui?: { nodes?: Array<{ attributes?: { name?: string; value?: string } }> }
}

interface KratosErrorBody {
  error?: { id?: string; code?: number; reason?: string; message?: string }
  ui?: { messages?: Array<{ id?: number; text?: string; type?: string }> }
  redirect_browser_to?: string
}

interface KratosSuccessBody {
  session?: { identity?: { id?: string; state?: string } }
  session_token?: string
}

const FLOW_TIMEOUT_MS = 5000

function csrfTokenOf(flow: KratosLoginFlow): string | undefined {
  return flow.ui?.nodes?.find((n) => n.attributes?.name === "csrf_token")
    ?.attributes?.value
}

/**
 * Kratos reports a refusal through numbered UI messages rather than a status
 * code of its own; 4000006 is the generic credential refusal, 4000010 an
 * inactive account, 4000002 a missing field. The address-unverified and
 * second-factor cases arrive as a redirect or an aal2 requirement instead.
 */
function outcomeFromMessages(body: KratosErrorBody): KratosOutcome | undefined {
  const ids = (body.ui?.messages ?? []).map((m) => m.id)
  if (ids.includes(4000010)) return { status: "account_disabled" }
  if (ids.includes(4000006) || ids.includes(4000002)) {
    return { status: "invalid_credentials" }
  }
  const errorId = body.error?.id
  if (errorId === "session_aal2_required") {
    return { status: "second_factor_required" }
  }
  if (errorId === "session_verified_address_required") {
    return { status: "email_not_verified" }
  }
  if (errorId === "browser_location_change_required") {
    return { status: "second_factor_required" }
  }
  return undefined
}

/**
 * Validates a password against Kratos through the native (API) login flow,
 * which returns the outcome as JSON instead of driving a browser.
 *
 * Every failure that is not an explicit refusal by Kratos answers
 * `unavailable`, never `invalid_credentials`: a person told their password is
 * wrong during an outage changes a password that was right.
 */
export async function verifyAgainstKratos(
  publicUrl: string,
  check: KratosCredentialCheck,
  fetchImpl: typeof fetch = fetch,
): Promise<KratosOutcome> {
  const base = publicUrl.replace(/\/$/, "")
  let flow: KratosLoginFlow
  try {
    const started = await fetchImpl(`${base}/self-service/login/api`, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal: AbortSignal.timeout(FLOW_TIMEOUT_MS),
    })
    if (!started.ok) return { status: "unavailable" }
    flow = (await started.json()) as KratosLoginFlow
    if (!flow?.id) return { status: "unavailable" }
  } catch {
    return { status: "unavailable" }
  }

  const csrf = csrfTokenOf(flow)
  try {
    const submitted = await fetchImpl(
      `${base}/self-service/login?flow=${encodeURIComponent(flow.id)}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({
          method: "password",
          identifier: check.email.trim().toLowerCase(),
          password: check.password,
          ...(csrf ? { csrf_token: csrf } : {}),
        }),
        signal: AbortSignal.timeout(FLOW_TIMEOUT_MS),
      },
    )

    if (submitted.ok) {
      const body = (await submitted.json()) as KratosSuccessBody
      const identity = body.session?.identity
      if (!identity?.id) return { status: "unavailable" }
      if (identity.state && identity.state !== "active") {
        return { status: "account_disabled" }
      }
      return { status: "valid", identityId: identity.id }
    }

    if (submitted.status >= 500) return { status: "unavailable" }

    const body = (await submitted.json().catch(() => ({}))) as KratosErrorBody
    const mapped = outcomeFromMessages(body)
    if (mapped) return mapped
    if (submitted.status === 400 || submitted.status === 401) {
      return { status: "invalid_credentials" }
    }
    if (submitted.status === 403) return { status: "second_factor_required" }
    return { status: "unavailable" }
  } catch {
    return { status: "unavailable" }
  }
}
