export const DEFAULT_HELP_ENDPOINT = 'https://nakoda.club';

export type HelpErrorKind = 'rate_limited' | 'unavailable' | 'unauthorized' | 'server' | 'network';

export class HelpError extends Error {
  constructor(
    readonly kind: HelpErrorKind,
    readonly status?: number,
  ) {
    super(status ? `help ${kind} (HTTP ${status})` : `help ${kind}`);
    this.name = 'HelpError';
  }
}

export interface HelpConfig {
  endpoint: string;
  app: string;
  fetch?: typeof fetch;
}

export interface HelpTurnRequest {
  conversation_id?: string;
  visitor_id: string;
  message: string;
}

export interface HelpTurnResponse {
  conversation_id: string;
  reply: string;
}

export interface HelpEscalateRequest {
  conversation_id: string;
  visitor_id: string;
  reason: string;
}

export interface HelpEscalateResponse {
  room: string;
  uri: string;
}

export interface HelpMessage {
  role: 'user' | 'assistant';
  text: string;
}

export function helpUrl(cfg: HelpConfig, action: 'turns' | 'escalate'): string {
  const base = (cfg.endpoint || DEFAULT_HELP_ENDPOINT).replace(/\/+$/, '');
  return `${base}/v1/help/${encodeURIComponent(cfg.app)}/${action}`;
}

export function errorKindForStatus(status: number): HelpErrorKind {
  if (status === 429) return 'rate_limited';
  if (status === 409) return 'unavailable';
  if (status === 401 || status === 403) return 'unauthorized';
  return 'server';
}

async function postJson<T>(cfg: HelpConfig, url: string, body: unknown, headers: Record<string, string> = {}): Promise<T> {
  const doFetch = cfg.fetch ?? fetch;
  let res: Response;
  try {
    res = await doFetch(url, {
      method: 'POST',
      credentials: 'omit',
      headers: { 'Content-Type': 'application/json', ...headers },
      body: JSON.stringify(body),
    });
  } catch {
    throw new HelpError('network');
  }
  if (!res.ok) throw new HelpError(errorKindForStatus(res.status), res.status);
  try {
    return (await res.json()) as T;
  } catch {
    throw new HelpError('server', res.status);
  }
}

export async function sendHelpTurn(cfg: HelpConfig, req: HelpTurnRequest): Promise<HelpTurnResponse> {
  const res = await postJson<HelpTurnResponse>(cfg, helpUrl(cfg, 'turns'), req);
  if (typeof res?.conversation_id !== 'string' || typeof res.reply !== 'string') throw new HelpError('server');
  return res;
}

export async function escalateHelp(
  cfg: HelpConfig,
  token: string,
  req: HelpEscalateRequest,
): Promise<HelpEscalateResponse> {
  const res = await postJson<HelpEscalateResponse>(cfg, helpUrl(cfg, 'escalate'), req, {
    Authorization: `Bearer ${token}`,
  });
  if (typeof res?.room !== 'string' || typeof res.uri !== 'string') throw new HelpError('server');
  return res;
}

type KeyValueStore = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

const memoryStore = new Map<string, string>();

function storageRead(store: () => KeyValueStore, key: string): string | null {
  try {
    const v = store().getItem(key);
    if (v !== null) return v;
  } catch {
    // storage blocked (private mode, sandboxed iframe): use the in-memory copy
  }
  return memoryStore.get(key) ?? null;
}

function storageWrite(store: () => KeyValueStore, key: string, value: string | null): void {
  if (value === null) memoryStore.delete(key);
  else memoryStore.set(key, value);
  try {
    if (value === null) store().removeItem(key);
    else store().setItem(key, value);
  } catch {
    // storage blocked: the in-memory copy lasts for this page
  }
}

const local = (): KeyValueStore => localStorage;
const session = (): KeyValueStore => sessionStorage;

function randomId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

export function visitorId(app: string, store: () => KeyValueStore = local): string {
  const key = `skalpai-help:visitor:${app}`;
  const existing = storageRead(store, key);
  if (existing) return existing;
  const id = randomId();
  storageWrite(store, key, id);
  return id;
}

export interface HelpThread {
  conversationId: string | null;
  messages: HelpMessage[];
}

export function loadThread(app: string, store: () => KeyValueStore = session): HelpThread {
  const raw = storageRead(store, `skalpai-help:thread:${app}`);
  if (!raw) return { conversationId: null, messages: [] };
  try {
    const parsed = JSON.parse(raw) as Partial<HelpThread>;
    const messages = Array.isArray(parsed.messages)
      ? parsed.messages.filter(
          (m): m is HelpMessage =>
            !!m && (m.role === 'user' || m.role === 'assistant') && typeof m.text === 'string',
        )
      : [];
    return { conversationId: typeof parsed.conversationId === 'string' ? parsed.conversationId : null, messages };
  } catch {
    return { conversationId: null, messages: [] };
  }
}

export function saveThread(app: string, thread: HelpThread, store: () => KeyValueStore = session): void {
  storageWrite(store, `skalpai-help:thread:${app}`, JSON.stringify(thread));
}

export type TextSegment = { text: string } | { text: string; href: string };

const URL_RE = /https?:\/\/[^\s<>"']+/g;
const TRAILING_PUNCT_RE = /[.,;:!?)\]}]+$/;

export function linkify(text: string): TextSegment[] {
  const out: TextSegment[] = [];
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    const start = m.index ?? 0;
    const url = m[0].replace(TRAILING_PUNCT_RE, '');
    if (start > last) out.push({ text: text.slice(last, start) });
    let safe = false;
    try {
      const proto = new URL(url).protocol;
      safe = proto === 'http:' || proto === 'https:';
    } catch {
      safe = false;
    }
    out.push(safe ? { text: url, href: url } : { text: url });
    last = start + url.length;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
}
