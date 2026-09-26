import { forwardHeaders } from "../core-proxy.ts";

// The path comes from the browser: it may only ever address the core, and the
// prefix is cut at a segment boundary, or `/api/core.evil.example` would
// become a host.
export function coreTarget(
  coreUrl: string,
  url: URL,
  stripPrefix: string | undefined,
): string | null {
  const prefix = stripPrefix?.replace(/\/$/, "");
  let path = url.pathname;
  if (prefix) {
    if (path === prefix) path = "/";
    else if (path.startsWith(`${prefix}/`)) path = path.slice(prefix.length);
    else return null;
  }
  return onCore(coreUrl, `${path}${url.search}`);
}

export function onCore(coreUrl: string, path: string): string | null {
  if (!path.startsWith("/") || path.startsWith("//")) return null;
  try {
    const target = new URL(`${coreUrl}${path}`);
    return target.origin === new URL(coreUrl).origin ? target.toString() : null;
  } catch {
    return null;
  }
}

export function keptCookies(
  headers: Headers,
  names: Array<string> | undefined,
): string | null {
  if (!names?.length) return null;
  const kept = (headers.get("cookie") ?? "")
    .split(";")
    .map((part) => part.trim())
    .filter((part) => names.includes(part.split("=")[0]));
  return kept.length ? kept.join("; ") : null;
}

// fetch hands back the body already decoded, so its content-encoding would
// have the browser decode it twice; an event stream must not be held by any
// buffering layer on the way.
export function relayedHeaders(upstream: Headers): Headers {
  const out = forwardHeaders(upstream);
  out.delete("set-cookie");
  out.delete("content-encoding");
  for (const c of upstream.getSetCookie?.() ?? []) out.append("set-cookie", c);
  if (out.get("content-type")?.includes("text/event-stream")) {
    out.set("cache-control", "no-cache, no-transform");
    out.set("x-accel-buffering", "no");
  }
  return out;
}
