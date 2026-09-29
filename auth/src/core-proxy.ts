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
