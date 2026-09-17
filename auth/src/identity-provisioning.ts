export interface IdentityProvisioningConfig {
  /** urbangate's issuer URL, e.g. https://id.urbangate.dev */
  issuer: string
  /** The product's provisioner client, e.g. "spore-provisioner". */
  clientId: string
  clientSecret: string
  /** The role granted on provisioning, e.g. "spore:user". */
  role: string
  /** The product id carried in the request, e.g. "spore". */
  product: string
}

export type ProvisionOutcome =
  | { status: "provisioned"; identityId: string; created: boolean }
  | { status: "rejected"; reason: string }
  | { status: "unavailable" }

export interface ProvisionRequest {
  email: string
  name?: string
  /**
   * The password the person just typed on the product's own form. Kratos
   * hashes it with the hasher its configuration declares, so an app cannot
   * hand over one it hashed itself; it is relayed for the length of this
   * request and stored nowhere. Omitted, the identity is created without a
   * credential and its owner sets one through recovery.
   */
  password?: string
}

interface TokenResponse {
  access_token?: string
  expires_in?: number
}

interface IdentityResponse {
  identity_id?: string
  created?: boolean
}

const REQUEST_TIMEOUT_MS = 5000
const TOKEN_EXPIRY_MARGIN_S = 30

interface CachedToken {
  value: string
  expiresAt: number
}

const tokenCache = new Map<string, CachedToken>()

export function resetProvisioningTokenCache(): void {
  tokenCache.clear()
}

async function accessToken(
  config: IdentityProvisioningConfig,
  fetchImpl: typeof fetch,
): Promise<string | undefined> {
  const key = `${config.issuer}|${config.clientId}`
  const cached = tokenCache.get(key)
  const now = Date.now() / 1000
  if (cached && cached.expiresAt > now) return cached.value

  const base = config.issuer.replace(/\/$/, "")
  const credentials = btoa(`${config.clientId}:${config.clientSecret}`)
  try {
    const response = await fetchImpl(`${base}/oauth2/token`, {
      method: "POST",
      headers: {
        Authorization: `Basic ${credentials}`,
        "Content-Type": "application/x-www-form-urlencoded",
      },
      body: new URLSearchParams({
        grant_type: "client_credentials",
        audience: "urbangate",
        scope: "urbangate:identities:provision",
      }).toString(),
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    })
    if (!response.ok) return undefined
    const body = (await response.json()) as TokenResponse
    if (!body.access_token) return undefined
    tokenCache.set(key, {
      value: body.access_token,
      expiresAt: now + (body.expires_in ?? 900) - TOKEN_EXPIRY_MARGIN_S,
    })
    return body.access_token
  } catch {
    return undefined
  }
}

/**
 * Creates or joins the person's identity at urbangate, returning the id the
 * local user row stores.
 *
 * The endpoint is idempotent on the address: a person who already has an
 * identity through another product of the suite gets that same one, with this
 * product's role added. The products therefore never own the identity, only
 * their role on it — a product deleting its local account must drop its role,
 * never deactivate the identity, or it would sign the person out of every
 * other product of the suite.
 */
export async function provisionIdentity(
  config: IdentityProvisioningConfig,
  request: ProvisionRequest,
  fetchImpl: typeof fetch = fetch,
): Promise<ProvisionOutcome> {
  const token = await accessToken(config, fetchImpl)
  if (!token) return { status: "unavailable" }

  const base = config.issuer.replace(/\/$/, "")
  try {
    const response = await fetchImpl(`${base}/api/machine/identities`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        email: request.email.trim().toLowerCase(),
        email_verified: true,
        name: request.name ?? "",
        role: config.role,
        product: config.product,
        ...(request.password ? { password: request.password } : {}),
      }),
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    })

    if (response.ok) {
      const body = (await response.json()) as IdentityResponse
      if (!body.identity_id) return { status: "unavailable" }
      return {
        status: "provisioned",
        identityId: body.identity_id,
        created: body.created === true,
      }
    }

    // 503 is the endpoint's retryable answer, including the inconclusive
    // lookup that refuses to risk a duplicate identity.
    if (response.status === 503 || response.status >= 500) {
      return { status: "unavailable" }
    }
    if (response.status === 401) {
      tokenCache.delete(`${config.issuer}|${config.clientId}`)
      return { status: "unavailable" }
    }
    const body = (await response.json().catch(() => ({}))) as {
      error?: string
      message?: string
    }
    return {
      status: "rejected",
      reason: body.error ?? body.message ?? `http_${response.status}`,
    }
  } catch {
    return { status: "unavailable" }
  }
}

/**
 * Sets the password of an identity the product already enrols, for a reset or
 * a change made on the product's own form.
 *
 * `rejected` with reason `not_found` is the person having no identity yet —
 * a local account that predates the move, or one whose provisioning is still
 * to be repaired — and is worth provisioning rather than retrying.
 */
export async function updateIdentityPassword(
  config: IdentityProvisioningConfig,
  request: { email: string; password: string },
  fetchImpl: typeof fetch = fetch,
): Promise<ProvisionOutcome> {
  const token = await accessToken(config, fetchImpl)
  if (!token) return { status: "unavailable" }

  const base = config.issuer.replace(/\/$/, "")
  try {
    const response = await fetchImpl(`${base}/api/machine/passwords`, {
      method: "PUT",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        email: request.email.trim().toLowerCase(),
        password: request.password,
      }),
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    })

    if (response.ok) {
      const body = (await response.json()) as IdentityResponse
      if (!body.identity_id) return { status: "unavailable" }
      return {
        status: "provisioned",
        identityId: body.identity_id,
        created: false,
      }
    }

    if (response.status === 503 || response.status >= 500) {
      return { status: "unavailable" }
    }
    if (response.status === 401) {
      tokenCache.delete(`${config.issuer}|${config.clientId}`)
      return { status: "unavailable" }
    }
    if (response.status === 404) {
      return { status: "rejected", reason: "not_found" }
    }
    const body = (await response.json().catch(() => ({}))) as {
      error?: string
      message?: string
    }
    return {
      status: "rejected",
      reason: body.error ?? body.message ?? `http_${response.status}`,
    }
  } catch {
    return { status: "unavailable" }
  }
}
