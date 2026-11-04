import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createTracker, readSource } from './tracker.ts';
import type { TrackerEnv } from './tracker.ts';

function env(start: string, referrer = '', hostname = 'shop.fr') {
  const sent: Array<{ url: string; body: Record<string, string> }> = [];
  const stored = new Map<string, string>();
  let href = start;
  const e: TrackerEnv = {
    href: () => href,
    hostname: () => hostname,
    referrer,
    send: (url, body) => sent.push({ url, body: JSON.parse(body) }),
    storage: { getItem: (k) => stored.get(k) ?? null, setItem: (k, v) => void stored.set(k, v) },
  };
  return { e, sent, stored, go: (next: string) => (href = next) };
}

test('the first page view carries the referrer, the next ones the previous page', () => {
  const { e, sent, go } = env('https://shop.fr/?utm_source=x', 'https://www.linkedin.com/');
  const t = createTracker({},e);
  t.pageview();
  go('https://shop.fr/pricing');
  t.pageview();
  assert.equal(sent[0].url, 'https://nakoda.club/v1/hit');
  assert.deepEqual(sent[0].body, { u: 'https://shop.fr/?utm_source=x', r: 'https://www.linkedin.com/' });
  assert.deepEqual(sent[1].body, { u: 'https://shop.fr/pricing', r: 'https://shop.fr/?utm_source=x' });
});

test('the same URL twice is one page view', () => {
  const { e, sent } = env('https://shop.fr/');
  const t = createTracker({},e);
  t.pageview();
  t.pageview();
  assert.equal(sent.length, 1);
});

test('the visit keeps where it started, the first landing only', () => {
  const first = env('https://shop.fr/?utm_source=newsletter', 'https://mail.google.com/');
  createTracker({},first.e);
  const again = { ...first.e, href: () => 'https://shop.fr/pricing', referrer: 'https://shop.fr/' };
  createTracker({},again);
  assert.deepEqual(readSource(first.e.storage), {
    landing: 'https://shop.fr/?utm_source=newsletter',
    referrer: 'https://mail.google.com/',
  });
});

test('a local page sends nothing, and the endpoint can be another nakoda', () => {
  const local = env('http://localhost:5273/', '', 'localhost');
  createTracker({},local.e).pageview();
  assert.equal(local.sent.length, 0);
  const own = env('https://shop.fr/');
  createTracker({ endpoint: 'https://stats.example/' }, own.e).pageview();
  assert.equal(own.sent[0].url, 'https://stats.example/v1/hit');
});

function device() {
  const kept = new Map<string, string>();
  return {
    kept,
    store: {
      getItem: (k: string) => kept.get(k) ?? null,
      setItem: (k: string, v: string) => void kept.set(k, v),
      removeItem: (k: string) => void kept.delete(k),
    },
  };
}

test('?nakoda=ignore stops this browser from being counted, for good', () => {
  const d = device();
  const { e, sent, go } = env('https://shop.fr/?nakoda=ignore');
  const t = createTracker({}, { ...e, device: d.store });
  t.pageview();
  go('https://shop.fr/pricing');
  t.pageview();
  assert.equal(sent.length, 0);
  const later = env('https://shop.fr/');
  createTracker({}, { ...later.e, device: d.store }).pageview();
  assert.equal(later.sent.length, 0);
});

test('?nakoda=count counts this browser again', () => {
  const d = device();
  d.store.setItem('nakoda.ignore', '1');
  const { e, sent } = env('https://shop.fr/?nakoda=count');
  createTracker({}, { ...e, device: d.store }).pageview();
  assert.equal(sent.length, 1);
  assert.equal(d.kept.size, 0);
});
