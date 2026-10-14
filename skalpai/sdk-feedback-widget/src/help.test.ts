import { describe, expect, it, vi } from 'vitest';
import {
  HelpError,
  escalateHelp,
  helpUrl,
  linkify,
  listRoomPosts,
  postToRoom,
  subjectOf,
  supportReplies,
  type RoomPost,
  loadThread,
  saveThread,
  sendHelpTurn,
  visitorId,
} from './help.js';

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function memStore() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

const blocked = () => {
  throw new Error('SecurityError');
};

describe('helpUrl', () => {
  it('defaults to nakoda and trims trailing slashes', () => {
    expect(helpUrl({ endpoint: '', app: 'lalter' }, 'turns')).toBe('https://nakoda.club/v1/help/lalter/turns');
    expect(helpUrl({ endpoint: 'http://x.test//', app: 'a b' }, 'escalate')).toBe('http://x.test/v1/help/a%20b/escalate');
  });
});

describe('sendHelpTurn', () => {
  it('posts JSON without credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { conversation_id: 'c1', reply: 'hi' }));
    const res = await sendHelpTurn(
      { endpoint: 'https://h.test', app: 'app', fetch: fetchMock },
      { visitor_id: 'v1', message: 'hello' },
    );
    expect(res).toEqual({ conversation_id: 'c1', reply: 'hi' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://h.test/v1/help/app/turns');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('omit');
    expect(init.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(JSON.parse(init.body)).toEqual({ visitor_id: 'v1', message: 'hello' });
  });

  it.each([
    [429, 'rate_limited'],
    [409, 'unavailable'],
    [401, 'unauthorized'],
    [500, 'server'],
    [503, 'server'],
  ])('maps HTTP %i to %s', async (status, kind) => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(status, {}));
    await expect(
      sendHelpTurn({ endpoint: '', app: 'a', fetch: fetchMock }, { visitor_id: 'v', message: 'm' }),
    ).rejects.toMatchObject({ kind, status });
  });

  it('maps a fetch failure to network', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    const err = await sendHelpTurn({ endpoint: '', app: 'a', fetch: fetchMock }, { visitor_id: 'v', message: 'm' }).catch(
      (e) => e,
    );
    expect(err).toBeInstanceOf(HelpError);
    expect(err.kind).toBe('network');
  });

  it('rejects a malformed body as server', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { reply: 1 }));
    await expect(
      sendHelpTurn({ endpoint: '', app: 'a', fetch: fetchMock }, { visitor_id: 'v', message: 'm' }),
    ).rejects.toMatchObject({ kind: 'server' });
  });
});

describe('escalateHelp', () => {
  it('sends the bearer token and the escalation body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { room: 'r1', uri: 'matrix:r/r1' }));
    const res = await escalateHelp({ endpoint: 'https://h.test', app: 'app', fetch: fetchMock }, 'tok', {
      conversation_id: 'c1',
      visitor_id: 'v1',
      reason: 'stuck',
    });
    expect(res).toEqual({ room: 'r1', uri: 'matrix:r/r1' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://h.test/v1/help/app/escalate');
    expect(init.headers.Authorization).toBe('Bearer tok');
    expect(init.credentials).toBe('omit');
    expect(JSON.parse(init.body)).toEqual({ conversation_id: 'c1', visitor_id: 'v1', reason: 'stuck' });
  });
});

describe('visitorId', () => {
  it('persists one id per app', () => {
    const store = memStore();
    const a = visitorId('app1', () => store);
    expect(a).toMatch(/^[0-9a-f-]{36}$/);
    expect(visitorId('app1', () => store)).toBe(a);
    expect(visitorId('app2', () => store)).not.toBe(a);
  });

  it('falls back to memory when storage is blocked', () => {
    const a = visitorId('blocked-app', blocked);
    expect(visitorId('blocked-app', blocked)).toBe(a);
  });
});

describe('thread persistence', () => {
  it('round-trips and drops invalid messages', () => {
    const store = memStore();
    saveThread('t', { conversationId: 'c', messages: [{ role: 'user', text: 'q' }] }, () => store);
    expect(loadThread('t', () => store)).toEqual({ conversationId: 'c', messages: [{ role: 'user', text: 'q' }] });
    store.setItem('skalpai-help:thread:x', JSON.stringify({ messages: [{ role: 'evil', text: 1 }] }));
    expect(loadThread('x', () => store)).toEqual({ conversationId: null, messages: [] });
    store.setItem('skalpai-help:thread:y', '{');
    expect(loadThread('y', () => store)).toEqual({ conversationId: null, messages: [] });
  });
});

describe('linkify', () => {
  it('splits http(s) links and strips trailing punctuation', () => {
    expect(linkify('See https://a.test/x. Then <b>')).toEqual([
      { text: 'See ' },
      { text: 'https://a.test/x', href: 'https://a.test/x' },
      { text: '. Then <b>' },
    ]);
  });

  it('never links other schemes', () => {
    expect(linkify('javascript:alert(1)')).toEqual([{ text: 'javascript:alert(1)' }]);
  });
});

describe('room mode', () => {
  const post = (seq: number, extra: Partial<RoomPost> = {}): RoomPost => ({
    seq,
    sender_sub: 'agent-1',
    sender_handle: 'Léa',
    body: `post ${seq}`,
    author: 'person',
    imported: false,
    status: 'published',
    ...extra,
  });

  it('keeps only new published replies from others, skipping the imported transcript', () => {
    const posts = [
      post(1, { imported: true, sender_sub: 'me' }),
      post(2, { imported: true, author: 'agent' }),
      post(3, { author: 'system', sender_sub: '', sender_handle: 'Support' }),
      post(4, { sender_sub: 'me' }),
      post(5),
      post(6, { status: 'pending' }),
    ];
    const { messages, lastSeq } = supportReplies(posts, 'me', 2);
    expect(lastSeq).toBe(6);
    expect(messages).toEqual([
      { role: 'support', text: 'post 3', who: 'Support' },
      { role: 'support', text: 'post 5', who: 'Léa' },
    ]);
    expect(supportReplies(posts, 'me', 6).messages).toEqual([]);
  });

  it('reads the subject of a bearer token', () => {
    const payload = btoa(JSON.stringify({ sub: 'person-1' })).replace(/=+$/, '');
    expect(subjectOf(`h.${payload}.s`)).toBe('person-1');
    expect(subjectOf('garbage')).toBe('');
  });

  it('lists and posts with the bearer token, without cookies', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify([post(2), post(1)]), { status: 200 }))
      .mockResolvedValueOnce(new Response(null, { status: 201 }));
    const cfg = { endpoint: 'https://chat.test/', fetch: fetchMock };
    const listed = await listRoomPosts(cfg, 'tok', 'r 1');
    expect(listed.map((p) => p.seq)).toEqual([1, 2]);
    await postToRoom(cfg, 'tok', 'r 1', 'merci');
    const [listUrl, listInit] = fetchMock.mock.calls[0];
    const [postUrl, postInit] = fetchMock.mock.calls[1];
    expect(listUrl).toBe('https://chat.test/v1/rooms/r%201/posts?limit=100');
    expect(listInit.credentials).toBe('omit');
    expect(listInit.headers.Authorization).toBe('Bearer tok');
    expect(postUrl).toBe('https://chat.test/v1/rooms/r%201/posts');
    expect(JSON.parse(postInit.body)).toEqual({ body: 'merci' });
  });

  it('round-trips the room in the stored thread', () => {
    const store = new Map<string, string>();
    const s = () => ({
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    });
    saveThread('app', { conversationId: 'c', messages: [{ role: 'support', text: 'hi', who: 'Léa' }], room: 'r', lastSeq: 4 }, s);
    expect(loadThread('app', s)).toEqual({ conversationId: 'c', messages: [{ role: 'support', text: 'hi', who: 'Léa' }], room: 'r', lastSeq: 4 });
  });
});
