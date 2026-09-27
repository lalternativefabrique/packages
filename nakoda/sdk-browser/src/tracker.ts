export interface NakodaOptions {
  /** The app's public site key, shown on its page in nakoda. */
  site: string;
  /** nakoda's origin; https://nakoda.club by default. */
  endpoint?: string;
}

/** Where the visit started, kept for the visit so a sign-up can say it. */
export interface VisitSource {
  landing: string;
  referrer: string;
}

export interface TrackerEnv {
  href: () => string;
  hostname: () => string;
  referrer: string;
  send: (url: string, body: string) => void;
  storage?: Pick<Storage, 'getItem' | 'setItem'>;
}

const DEFAULT_ENDPOINT = 'https://nakoda.club';
const SOURCE_KEY = 'nakoda.source';
const LOCAL_HOSTS = ['localhost', '127.0.0.1', '[::1]'];

export function isLocal(hostname: string): boolean {
  return LOCAL_HOSTS.includes(hostname) || hostname.endsWith('.localhost');
}

export function readSource(storage: TrackerEnv['storage']): VisitSource | null {
  try {
    const raw = storage?.getItem(SOURCE_KEY);
    return raw ? (JSON.parse(raw) as VisitSource) : null;
  } catch {
    return null;
  }
}

/**
 * A tracker sends one page view per distinct URL. The first carries the
 * page's referrer; the next ones, the previous URL of the same site, which
 * nakoda reads as the same visit going on.
 */
export function createTracker(options: NakodaOptions, env: TrackerEnv) {
  const endpoint = (options.endpoint ?? DEFAULT_ENDPOINT).replace(/\/$/, '') + '/v1/hit';
  let previous: string | null = null;

  if (!readSource(env.storage)) {
    try {
      env.storage?.setItem(SOURCE_KEY, JSON.stringify({ landing: env.href(), referrer: env.referrer }));
    } catch {
      // A browser that refuses storage still counts its page views.
    }
  }

  return {
    pageview(): void {
      const href = env.href();
      if (href === previous || isLocal(env.hostname())) return;
      const referrer = previous ?? env.referrer;
      previous = href;
      env.send(endpoint, JSON.stringify({ s: options.site, u: href, r: referrer }));
    },
  };
}
