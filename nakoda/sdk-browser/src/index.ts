import { createTracker, readSource } from './tracker.ts';
import type { NakodaOptions, VisitSource } from './tracker.ts';

export type { NakodaOptions, VisitSource };

function send(url: string, body: string): void {
  // text/plain keeps the request simple: no CORS preflight, and sendBeacon
  // outlives the page being left.
  const blob = new Blob([body], { type: 'text/plain' });
  if (navigator.sendBeacon?.(url, blob)) return;
  void fetch(url, { method: 'POST', body, keepalive: true, headers: { 'content-type': 'text/plain' } }).catch(
    () => undefined,
  );
}

function storage(): Storage | undefined {
  try {
    return window.sessionStorage;
  } catch {
    return undefined;
  }
}

/**
 * Counts the page's views with nakoda, the History API's navigations
 * included, and keeps where the visit came from. Returns what stops it.
 */
export function init(options: NakodaOptions): () => void {
  if (typeof window === 'undefined') return () => undefined;
  const tracker = createTracker(options, {
    href: () => location.href,
    hostname: () => location.hostname,
    referrer: document.referrer,
    send,
    storage: storage(),
  });
  tracker.pageview();

  const pushState = history.pushState;
  const replaceState = history.replaceState;
  history.pushState = function (this: History, ...args: Parameters<History['pushState']>) {
    pushState.apply(this, args);
    tracker.pageview();
  };
  history.replaceState = function (this: History, ...args: Parameters<History['replaceState']>) {
    replaceState.apply(this, args);
    tracker.pageview();
  };
  const onPop = () => tracker.pageview();
  window.addEventListener('popstate', onPop);

  return () => {
    history.pushState = pushState;
    history.replaceState = replaceState;
    window.removeEventListener('popstate', onPop);
  };
}

/** Where the current visit started: the landing URL and its referrer. */
export function getSource(): VisitSource | null {
  if (typeof window === 'undefined') return null;
  return readSource(storage());
}
