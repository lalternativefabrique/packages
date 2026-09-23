export interface CookieOptions {
  maxAge: number;
  secure: boolean;
}

export function readCookie(headers: Headers, name: string): string | undefined {
  const raw = headers.get("cookie") ?? "";
  for (const part of raw.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return decodeURIComponent(v.join("="));
  }
  return undefined;
}

export function serializeCookie(
  name: string,
  value: string,
  o: CookieOptions,
): string {
  const attrs = [
    `${name}=${encodeURIComponent(value)}`,
    "Path=/",
    "HttpOnly",
    "SameSite=Lax",
    `Max-Age=${value ? o.maxAge : 0}`,
  ];
  if (o.secure) attrs.push("Secure");
  return attrs.join("; ");
}

export function clearCookie(name: string, secure: boolean): string {
  return serializeCookie(name, "", { maxAge: 0, secure });
}
