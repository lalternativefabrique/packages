import type { FeedbackType } from './events';

export interface FeedbackAttachment {
  uri: string;
  name: string;
  mimeType: string;
  size?: number;
}

export const MAX_ATTACHMENTS = 5;
export const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
export const ALLOWED_MIME = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
const MAX_CONTEXT_VALUE_CHARS = 2000;

export const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export type AttachmentRejection = 'too_many' | 'too_large' | 'unsupported';

export function checkAttachment(
  attachment: FeedbackAttachment,
  currentCount: number,
): AttachmentRejection | null {
  if (currentCount >= MAX_ATTACHMENTS) return 'too_many';
  if (!ALLOWED_MIME.includes(attachment.mimeType)) return 'unsupported';
  if (attachment.size !== undefined && attachment.size > MAX_ATTACHMENT_BYTES) return 'too_large';
  return null;
}

export function formatContext(context: Record<string, string>): string {
  const lines = Object.entries(context).map(
    ([k, v]) => `${k}: ${v.length > MAX_CONTEXT_VALUE_CHARS ? v.slice(0, MAX_CONTEXT_VALUE_CHARS) + '…' : v}`,
  );
  return lines.length ? `\n\n---\n${lines.join('\n')}` : '';
}

export interface FeedbackSubmission {
  endpoint: string;
  projectId: string;
  apiKey?: string;
  headers?: Record<string, string>;
  type: FeedbackType;
  message: string;
  source: string;
  context: Record<string, string>;
  path: string;
  userIdentifier: string;
  attachments: FeedbackAttachment[];
}

export function submitUrl(endpoint: string, projectId: string): string {
  return `${endpoint.replace(/\/$/, '')}/api/projects/${encodeURIComponent(projectId)}/issues/submit`;
}

export function buildForm(s: FeedbackSubmission): FormData {
  const form = new FormData();
  form.append('type', s.type);
  form.append('message', s.message + formatContext(s.context));
  form.append('source', s.source);
  if (Object.keys(s.context).length) form.append('context', JSON.stringify(s.context));
  form.append('url', s.path);
  form.append('device', 'mobile');
  form.append('user_identifier', s.userIdentifier.trim());
  for (const a of s.attachments) form.append('attachments', { uri: a.uri, name: a.name, type: a.mimeType });
  return form;
}

export async function submitFeedback(
  s: FeedbackSubmission,
  fetchImpl: typeof fetch = fetch,
): Promise<void> {
  const res = await fetchImpl(submitUrl(s.endpoint, s.projectId), {
    method: 'POST',
    body: buildForm(s),
    headers: { ...s.headers, ...(s.apiKey ? { 'x-api-key': s.apiKey } : {}) },
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}
