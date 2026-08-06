import type { InvitationFailure } from "./types"

/**
 * What became of a claim, in terms the invitee can be told.
 *
 * 'expired' and 'claimed' are kept apart from 'unknown' because only they say
 * the offer was real, which is what tells someone that asking for a new link is
 * worth it rather than doubting the address they were invited at. The three
 * failures match InvitationFailure, so an outcome feeds InvitationNotice
 * directly.
 */
export type ClaimOutcome = "granted" | InvitationFailure | "failed"

const OUTCOME_BY_STATUS: Record<number, ClaimOutcome> = {
  404: "unknown",
  409: "claimed",
  410: "expired",
}

export interface ClaimInvitationOptions {
  /** Absolute URL of the endpoint that redeems a token. */
  endpoint: string
  token: string
  /** The account the app just created, which the grant is attached to. */
  externalUserId: string
  /** Sent as the Authorization bearer — typically the app's API key. */
  apiKey?: string
  /** Merged into the request body, for backends wanting more than the token. */
  extra?: Record<string, unknown>
  /** Headers merged last, so a caller can pass a cookie-based credential. */
  headers?: Record<string, string>
  /** Bounds the call so a slow API never stalls the sign-in response. */
  timeoutMs?: number
}

/**
 * Redeems an invitation token for a user who has just signed in, turning the
 * offer into a grant on their account.
 *
 * Why this belongs on the SERVER, on the auth callback rather than in the page:
 * the invitation link lands on /register?invite=<token>, but the sign-up that
 * follows can complete through any of three flows (password + OTP, OAuth
 * redirect, email verification), and only two of them return to the page that
 * held the token. Claiming where the session is established covers every flow
 * with one code path.
 *
 * Best-effort by design: a sign-in must never fail because an invitation could
 * not be redeemed. A failed claim leaves the invitation unclaimed and the user
 * on their default tier — recoverable by following the link again, since a
 * refused claim consumes nothing.
 */
export async function claimInvitation({
  endpoint,
  token,
  externalUserId,
  apiKey,
  extra,
  headers,
  timeoutMs = 5000,
}: ClaimInvitationOptions): Promise<ClaimOutcome> {
  try {
    const res = await fetch(endpoint, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...(apiKey ? { authorization: `Bearer ${apiKey}` } : {}),
        ...headers,
      },
      body: JSON.stringify({
        token,
        external_user_id: externalUserId,
        ...extra,
      }),
      signal: AbortSignal.timeout(timeoutMs),
    })

    if (res.ok) return "granted"
    return OUTCOME_BY_STATUS[res.status] ?? "failed"
  } catch {
    return "failed"
  }
}

/**
 * Extracts the invitation token from an auth request.
 *
 * The token lives on the page the invitee landed on (/register?invite=…), never
 * on the auth endpoints themselves, so it has to be recovered from the request
 * that completes the sign-up. Two sources, because no single one covers every
 * flow: the URL, for the OAuth callback reached through a redirect whose query
 * string the app controls; and the Referer, for the password and OTP flows,
 * which are XHR calls issued BY that page and therefore carry it.
 *
 * Reading it per-request keeps the claim stateless: nothing associates a
 * browser with a pending invitation, and a request with no token claims
 * nothing. A malformed Referer is ignored rather than thrown on — it is
 * attacker-controlled input on a best-effort path.
 */
export function inviteTokenFrom(
  request: Request,
  param = "invite",
): string | null {
  const direct = new URL(request.url).searchParams.get(param)
  if (direct && direct.trim() !== "") return direct

  const referer = request.headers.get("referer")
  if (!referer) return null
  try {
    const token = new URL(referer).searchParams.get(param)
    return token && token.trim() !== "" ? token : null
  } catch {
    return null
  }
}

/**
 * BetterAuth paths that complete a sign-up. Email/password returns the user
 * straight from sign-up/email; the OTP and OAuth flows only produce a usable
 * account once the verification/callback succeeds, so those are the moments
 * worth reacting to.
 */
const SIGNUP_COMPLETING = [
  "/sign-up/email",
  "/sign-in/email-otp",
  "/email-otp/verify-email",
  "/callback/",
]

/**
 * Whether this request is the one that just created a usable account — the
 * moment to provision, claim an invitation, or greet someone. Matching on the
 * path rather than on a response body keeps it flow-agnostic: the three
 * sign-up flows return three different shapes.
 */
export function completesSignup(pathname: string): boolean {
  return SIGNUP_COMPLETING.some((p) => pathname.includes(p))
}

/**
 * Carries a failed claim to the next page. The claim happens inside an auth
 * response nobody renders, so its result would otherwise reach only the server
 * log — leaving an invitee on the default tier with no idea their link had
 * lapsed. Short-lived and readable by the page, which reports it and clears it.
 */
export function invitationOutcomeCookie(
  outcome: ClaimOutcome,
  name = "invite_claim",
): string {
  return `${name}=${outcome}; Path=/; Max-Age=120; SameSite=Lax`
}

/**
 * Whether an outcome is one the invitee should be shown a reason for.
 * 'failed' is excluded: it means the call did not complete, so the offer may
 * still be good and telling someone their invitation is invalid would be wrong.
 */
export function isInvitationFailure(
  outcome: ClaimOutcome,
): outcome is InvitationFailure {
  return outcome === "expired" || outcome === "claimed" || outcome === "unknown"
}
