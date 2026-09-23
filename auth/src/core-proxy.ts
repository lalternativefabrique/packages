import type { PlatformSession } from "./types"

type ProxyAuth = {
  api: {
    getSession: (ctx: { headers: Headers }) => Promise<unknown>
    getAccessToken: (ctx: {
      body: { providerId: string }
      headers: Headers
    }) => Promise<{ accessToken?: string } | null>
  }
}

export interface CoreProxyOptions {
  /** Where the core listens inside the space, e.g. http://app:8080. */
  coreUrl: string
  /** Better Auth provider id of urbangate. Defaults to "urbangate". */
  providerId?: string
  /** Refuse a session whose role is not admin. Defaults to false. */
  adminOnly?: boolean
  /**
   * The token to send for a session that holds no urbangate token — an
   * account that signed in with a local password. Omitted, such a session is
   * answered 401 sign_in_required, which is where ADR 0009 ends up; an app
   * still moving keeps minting its own token here meanwhile.
   */
  fallbackToken?: (headers: Headers) => Promise<string>
}

// Hop-by-hop headers and the cookie stay on this side: the core gets one
// Authorization header and nothing that named the browser's session.
const DROPPED = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "host",
  "content-length",
  "cookie",
  "authorization",
])

export function forwardHeaders(headers: Headers): Headers {
  const out = new Headers()
  headers.forEach((value, key) => {
    if (!DROPPED.has(key.toLowerCase())) out.set(key, value)
  })
  return out
}

function json(status: number, error: string): Response {
  return new Response(JSON.stringify({ error }), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

/**
 * Same-origin proxy from an app's web to its core. The browser never holds a
 * token: the session is exchanged here for the access token urbangate issued
 * at sign-in, refreshed by Better Auth when it is about to expire, and the
 * core verifies it against urbangate's key set with the product as audience.
 */
export function coreProxy(auth: ProxyAuth, options: CoreProxyOptions) {
  const providerId = options.providerId ?? "urbangate"
  const coreUrl = options.coreUrl.replace(/\/$/, "")

  return async (request: Request): Promise<Response> => {
    const session = (await auth.api.getSession({
      headers: request.headers,
    })) as PlatformSession | null
    if (!session) return json(401, "unauthorized")
    if (options.adminOnly && session.user.role !== "admin") {
      return json(403, "forbidden")
    }

    const token = await accessToken(auth, providerId, request.headers, options.fallbackToken)
    if (!token) return json(401, "sign_in_required")

    const url = new URL(request.url)
    const headers = forwardHeaders(request.headers)
    headers.set("Authorization", `Bearer ${token}`)
    const upstream = await fetch(`${coreUrl}${url.pathname}${url.search}`, {
      method: request.method,
      headers,
      body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
      duplex: "half",
      redirect: "manual",
    } as RequestInit)
    return new Response(upstream.body, {
      status: upstream.status,
      headers: forwardHeaders(upstream.headers),
    })
  }
}

async function accessToken(
  auth: ProxyAuth,
  providerId: string,
  headers: Headers,
  fallback: CoreProxyOptions["fallbackToken"],
): Promise<string | null> {
  try {
    const tokens = await auth.api.getAccessToken({ body: { providerId }, headers })
    if (tokens?.accessToken) return tokens.accessToken
  } catch {
    // No account at the provider, or a refresh Hydra refused: the fallback
    // decides, and without one the person signs in again.
  }
  return fallback ? fallback(headers) : null
}
