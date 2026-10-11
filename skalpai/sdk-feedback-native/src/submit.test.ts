import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  MAX_ATTACHMENTS,
  MAX_ATTACHMENT_BYTES,
  buildForm,
  checkAttachment,
  formatContext,
  submitFeedback,
  submitUrl,
  type FeedbackSubmission,
} from './submit';

class RecordingFormData {
  entries: Array<[string, unknown]> = [];
  append(name: string, value: unknown) {
    this.entries.push([name, value]);
  }
  get(name: string) {
    return this.entries.find(([n]) => n === name)?.[1];
  }
}

const base: FeedbackSubmission = {
  endpoint: 'https://api.example.com/',
  projectId: 'p 1',
  apiKey: 'k',
  type: 'bug',
  message: 'Broken',
  source: 'manual',
  context: {},
  path: '/settings',
  userIdentifier: ' a@b.co ',
  attachments: [],
};

afterEach(() => vi.unstubAllGlobals());

describe('submitUrl', () => {
  it('strips the trailing slash and encodes the project id', () => {
    expect(submitUrl(base.endpoint, base.projectId)).toBe('https://api.example.com/api/projects/p%201/issues/submit');
  });
});

describe('formatContext', () => {
  it('is empty without context', () => {
    expect(formatContext({})).toBe('');
  });

  it('truncates long values', () => {
    const out = formatContext({ stack: 'x'.repeat(2500) });
    expect(out.startsWith('\n\n---\nstack: ')).toBe(true);
    expect(out.endsWith('…')).toBe(true);
    expect(out.length).toBeLessThan(2100);
  });
});

describe('checkAttachment', () => {
  const ok = { uri: 'file:///a.jpg', name: 'a.jpg', mimeType: 'image/jpeg', size: 10 };

  it('accepts a small image', () => {
    expect(checkAttachment(ok, 0)).toBeNull();
  });

  it('rejects past the attachment cap', () => {
    expect(checkAttachment(ok, MAX_ATTACHMENTS)).toBe('too_many');
  });

  it('rejects non-web image formats', () => {
    expect(checkAttachment({ ...ok, mimeType: 'image/heic' }, 0)).toBe('unsupported');
  });

  it('rejects oversized files and lets unknown sizes through', () => {
    expect(checkAttachment({ ...ok, size: MAX_ATTACHMENT_BYTES + 1 }, 0)).toBe('too_large');
    expect(checkAttachment({ ...ok, size: undefined }, 0)).toBeNull();
  });
});

describe('buildForm', () => {
  it('folds the context into the message, since relays only forward known fields', () => {
    vi.stubGlobal('FormData', RecordingFormData);
    const form = buildForm({ ...base, context: { os: 'ios 18.2' } }) as unknown as RecordingFormData;
    expect(form.get('message')).toBe('Broken\n\n---\nos: ios 18.2');
    expect(form.get('context')).toBe('{"os":"ios 18.2"}');
    expect(form.get('device')).toBe('mobile');
    expect(form.get('url')).toBe('/settings');
    expect(form.get('user_identifier')).toBe('a@b.co');
  });

  it('sends attachments as React Native file descriptors', () => {
    vi.stubGlobal('FormData', RecordingFormData);
    const form = buildForm({
      ...base,
      attachments: [{ uri: 'file:///s.jpg', name: 'screenshot.jpg', mimeType: 'image/jpeg' }],
    }) as unknown as RecordingFormData;
    expect(form.entries.filter(([n]) => n === 'attachments')).toEqual([
      ['attachments', { uri: 'file:///s.jpg', name: 'screenshot.jpg', type: 'image/jpeg' }],
    ]);
    expect(form.get('context')).toBeUndefined();
  });
});

describe('submitFeedback', () => {
  it('posts with the api key header', async () => {
    vi.stubGlobal('FormData', RecordingFormData);
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    await submitFeedback(base, fetchImpl as unknown as typeof fetch);
    const [url, init] = fetchImpl.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/projects/p%201/issues/submit');
    expect(init.method).toBe('POST');
    expect(init.headers).toEqual({ 'x-api-key': 'k' });
  });

  it('merges host headers such as a session cookie', async () => {
    vi.stubGlobal('FormData', RecordingFormData);
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    await submitFeedback({ ...base, headers: { cookie: 'session=abc' } }, fetchImpl as unknown as typeof fetch);
    expect(fetchImpl.mock.calls[0][1].headers).toEqual({ cookie: 'session=abc', 'x-api-key': 'k' });
  });

  it('throws on a non-2xx response', async () => {
    vi.stubGlobal('FormData', RecordingFormData);
    const fetchImpl = vi.fn().mockResolvedValue({ ok: false, status: 503 });
    await expect(submitFeedback({ ...base, apiKey: undefined }, fetchImpl as unknown as typeof fetch)).rejects.toThrow(
      'HTTP 503',
    );
    expect(fetchImpl.mock.calls[0][1].headers).toEqual({});
  });
});
