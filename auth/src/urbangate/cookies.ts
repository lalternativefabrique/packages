export interface CookieOptions {
  maxAge: number;
  secure: boolean;
  /** Set to share the session across an apex domain's sub-domains, e.g. ".messag.eco". */
  domain?: string;
}

export function readCookie(headers: Headers, name: string): string | undefined {
  const raw = headers.get("cookie") ?? "";
  for (const part of raw.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return decodeURIComponent(v.join("="));
  }
  return undefined;
}

/**
 * A cookie domain, e.g. `messag.eco` or a leading-dot `.messag.eco`. `name`
 * and `value` are encoded, but the domain is concatenated into the header as
 * it is, so an unchecked value could smuggle extra attributes or, with a CRLF,
 * a whole second `Set-Cookie`. It is meant to come from configuration, not a
 * request; validating it keeps that true even when a caller wires it wrong.
 */
const DOMAIN = /^\.?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$/i;

export function serializeCookie(
  name: string,
  value: string,
  o: CookieOptions,
): string {
  if (o.domain !== undefined && !DOMAIN.test(o.domain)) {
    throw new Error(`invalid cookie domain: ${JSON.stringify(o.domain)}`);
  }
  const attrs = [
    `${name}=${encodeURIComponent(value)}`,
    "Path=/",
    "HttpOnly",
    "SameSite=Lax",
    `Max-Age=${value ? o.maxAge : 0}`,
  ];
  if (o.domain) attrs.push(`Domain=${o.domain}`);
  if (o.secure) attrs.push("Secure");
  return attrs.join("; ");
}

export function clearCookie(
  name: string,
  secure: boolean,
  domain?: string,
): string {
  return serializeCookie(name, "", { maxAge: 0, secure, domain });
}
