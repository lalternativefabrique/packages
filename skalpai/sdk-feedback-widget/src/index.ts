type FeedbackType = 'bug' | 'idea' | 'other';

export type Placement = 'bottom-left' | 'bottom-right' | 'inline';

type Labels = {
  title: string;
  send: string;
  sending: string;
  close: string;
  bug: string;
  idea: string;
  other: string;
  placeholder: string;
  thanks: string;
  capture: string;
  capturing: string;
  remove_screenshot: string;
  attach: string;
  remove_attachment: string;
  email_placeholder: string;
  email_invalid: string;
};

const DEFAULT_LABELS: Labels = {
  title: 'Feedback',
  send: 'Envoyer',
  sending: 'Envoi…',
  close: 'Fermer',
  bug: 'Bug',
  idea: 'Idée',
  other: 'Autre',
  placeholder: 'Décrivez votre retour…',
  thanks: 'Merci pour votre retour !',
  capture: 'Capturer l’écran',
  capturing: 'Capture…',
  remove_screenshot: 'Retirer la capture',
  attach: 'Joindre une image',
  remove_attachment: 'Retirer la pièce jointe',
  email_placeholder: 'Votre email (pour une réponse)',
  email_invalid: 'Email invalide',
};

const MAX_ATTACHMENTS = 5;
const ALLOWED_MIME = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
// Per-attachment cap matches the server default (ATTACHMENT_MAX_BYTES, 5 MiB).
const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
// Compression target for screenshots and oversized images.
const COMPRESS_MAX_DIMENSION = 1920;
const COMPRESS_QUALITY = 0.85;
// Threshold above which a user-picked image is recompressed to fit the cap.
const COMPRESS_THRESHOLD_BYTES = 1_500_000;

const STYLES = `
  :host {
    all: initial;
    font-family: system-ui, -apple-system, sans-serif;
    --skalpai-fab-inset-block: 16px;
    --skalpai-fab-inset-inline: 16px;
    --skalpai-panel-gap: 48px;
    --skalpai-bg: #ffffff;
    --skalpai-fg: #111111;
    --skalpai-panel: #ffffff;
    --skalpai-input: #ffffff;
    --skalpai-border: rgba(0,0,0,.1);
    --skalpai-border-soft: rgba(0,0,0,.06);
    --skalpai-muted: rgba(0,0,0,.4);
    --skalpai-muted-soft: rgba(0,0,0,.06);
    --skalpai-muted-soft-hover: rgba(0,0,0,.1);
    --skalpai-accent: #7c3aed;
    --skalpai-disabled: rgba(0,0,0,.08);
    --skalpai-disabled-fg: rgba(0,0,0,.35);
  }
  :host([data-theme="dark"]) {
    --skalpai-bg: #1a1a1a;
    --skalpai-fg: #f5f5f5;
    --skalpai-panel: #1f1f1f;
    --skalpai-input: #2a2a2a;
    --skalpai-border: rgba(255,255,255,.08);
    --skalpai-border-soft: rgba(255,255,255,.05);
    --skalpai-muted: rgba(255,255,255,.45);
    --skalpai-muted-soft: rgba(255,255,255,.06);
    --skalpai-muted-soft-hover: rgba(255,255,255,.1);
    --skalpai-disabled: rgba(255,255,255,.08);
    --skalpai-disabled-fg: rgba(255,255,255,.35);
  }
  *, *::before, *::after { box-sizing: border-box; }
  .btn-fab {
    position: fixed;
    bottom: var(--skalpai-fab-inset-block);
    left: var(--skalpai-fab-inset-inline);
    z-index: 2147483646;
    display: inline-flex; align-items: center; gap: 8px;
    padding: 8px 14px; border: 0; border-radius: 999px;
    background: var(--skalpai-fg); color: var(--skalpai-bg);
    font-size: 13px; font-weight: 500; cursor: pointer;
    box-shadow: 0 4px 12px rgba(0,0,0,.25);
    transition: opacity .15s;
  }
  .btn-fab:hover { opacity: .9; }
  .panel {
    position: fixed;
    bottom: calc(var(--skalpai-fab-inset-block) + var(--skalpai-panel-gap));
    left: var(--skalpai-fab-inset-inline);
    z-index: 2147483647;
    width: 320px; max-width: calc(100vw - 32px);
    background: var(--skalpai-panel); color: var(--skalpai-fg);
    border: 1px solid var(--skalpai-border); border-radius: 12px;
    box-shadow: 0 8px 32px rgba(0,0,0,.35); overflow: hidden;
  }

  :host([placement="bottom-right"]) .btn-fab,
  :host([placement="bottom-right"]) .panel {
    left: auto;
    right: var(--skalpai-fab-inset-inline);
  }

  :host([placement="inline"]) {
    position: relative;
    display: inline-flex;
  }
  :host([placement="inline"]) .btn-fab {
    position: static;
    width: 100%;
    box-shadow: none;
  }
  :host([placement="inline"]) .panel {
    position: absolute;
    bottom: calc(100% + 8px);
    left: 0;
    right: auto;
    max-width: min(320px, calc(100vw - 32px));
  }
  .head {
    display: flex; justify-content: space-between; align-items: center;
    padding: 12px 16px; border-bottom: 1px solid var(--skalpai-border-soft);
    font-size: 14px; font-weight: 500;
  }
  .close {
    background: none; border: 0; cursor: pointer; font-size: 18px;
    color: var(--skalpai-muted); padding: 2px 8px; border-radius: 4px; line-height: 1;
  }
  .close:hover { background: var(--skalpai-muted-soft); color: var(--skalpai-fg); }
  .body { padding: 14px 16px 16px; display: flex; flex-direction: column; gap: 12px; }
  .types { display: flex; gap: 6px; }
  .type {
    flex: 0 0 auto; padding: 6px 14px; border: 0; border-radius: 8px;
    background: var(--skalpai-muted-soft); color: var(--skalpai-fg);
    font-size: 13px; font-weight: 500; cursor: pointer;
    transition: background .15s;
  }
  .type:hover { background: var(--skalpai-muted-soft-hover); }
  .type[aria-pressed="true"] { background: var(--skalpai-fg); color: var(--skalpai-bg); }
  textarea {
    width: 100%; min-height: 90px; padding: 10px 12px;
    border: 1px solid var(--skalpai-border); border-radius: 8px;
    font: inherit; font-size: 13px; resize: vertical;
    background: var(--skalpai-input); color: var(--skalpai-fg);
  }
  textarea::placeholder { color: var(--skalpai-muted); }
  textarea:focus {
    outline: 2px solid var(--skalpai-accent);
    outline-offset: -1px; border-color: transparent;
  }
  .email {
    width: 100%; padding: 9px 12px;
    border: 1px solid var(--skalpai-border); border-radius: 8px;
    font: inherit; font-size: 13px;
    background: var(--skalpai-input); color: var(--skalpai-fg);
  }
  .email::placeholder { color: var(--skalpai-muted); }
  .email:focus {
    outline: 2px solid var(--skalpai-accent);
    outline-offset: -1px; border-color: transparent;
  }
  .email-hint { font-size: 11px; color: #f87171; margin-top: -6px; }
  .meta { font-size: 11px; color: var(--skalpai-muted); word-break: break-all; line-height: 1.5; }
  .capture-row { display: flex; align-items: center; gap: 8px; }
  .capture-btn {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 6px 10px; border: 1px solid var(--skalpai-border); border-radius: 8px;
    background: transparent; color: var(--skalpai-fg); font-size: 12px; cursor: pointer;
    transition: background .15s;
  }
  .capture-btn:hover { background: var(--skalpai-muted-soft); }
  .capture-btn:disabled { opacity: .5; cursor: not-allowed; }
  .preview {
    position: relative; display: inline-block;
    border: 1px solid var(--skalpai-border); border-radius: 8px; overflow: hidden;
  }
  .preview img { display: block; width: 64px; height: 48px; object-fit: cover; }
  .attach-list { display: flex; flex-wrap: wrap; gap: 6px; }
  .preview-remove {
    position: absolute; top: 2px; right: 2px;
    width: 18px; height: 18px; border-radius: 50%;
    border: 0; background: rgba(0,0,0,.7); color: #fff;
    font-size: 12px; line-height: 1; cursor: pointer; padding: 0;
  }
  .submit {
    width: 100%; padding: 10px; border: 0; border-radius: 8px;
    background: var(--skalpai-fg); color: var(--skalpai-bg);
    font-size: 13px; font-weight: 500; cursor: pointer;
    transition: opacity .15s;
  }
  .submit:hover:not(:disabled) { opacity: .9; }
  .submit:disabled {
    background: var(--skalpai-disabled); color: var(--skalpai-disabled-fg); cursor: not-allowed;
  }
  .err {
    padding: 8px 10px; background: rgba(239,68,68,.12); color: #f87171;
    font-size: 12px; border-radius: 6px;
  }
  .ok { padding: 28px 16px; text-align: center; font-size: 13px; color: var(--skalpai-muted); }
`;

// Lightweight client-side email check. The server re-validates before any
// reply is sent (contact_user), so this only guards the UX, not security.
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!),
  );
}

const HTMLElementCtor: typeof HTMLElement =
  typeof HTMLElement !== 'undefined'
    ? HTMLElement
    : (class {} as unknown as typeof HTMLElement);

export class SkalpaiFeedbackElement extends HTMLElementCtor {
  static observedAttributes = ['api-key', 'endpoint', 'project-id', 'labels', 'theme', 'user-email', 'placement'];

  private root: ShadowRoot;
  private open = false;
  private type: FeedbackType = 'other';
  private message = '';
  private state: 'idle' | 'loading' | 'sent' | 'error' = 'idle';
  private errorMsg = '';
  private attachments: Array<{ blob: Blob; previewUrl: string; name: string }> = [];
  private capturing = false;
  private themeObserver: MutationObserver | null = null;
  private mqlDark: MediaQueryList | null = null;

  /**
   * Optional user email used as the reply-to identity. Pre-filled from the
   * `user-email` attribute (set by the host app for logged-in users) and
   * editable by the visitor. Left empty for anonymous feedback — the entry is
   * still recorded, but the project owner cannot reply by email.
   */
  userIdentifier = '';

  /** Whether the email field was edited/blurred — gates the invalid hint. */
  private emailTouched = false;

  get apiKey(): string {
    return this.getAttribute('api-key') ?? '';
  }
  set apiKey(v: string) {
    if (v) this.setAttribute('api-key', v);
    else this.removeAttribute('api-key');
  }

  get endpoint(): string {
    return this.getAttribute('endpoint') ?? '';
  }
  set endpoint(v: string) {
    if (v) this.setAttribute('endpoint', v);
    else this.removeAttribute('endpoint');
  }

  get projectId(): string {
    return this.getAttribute('project-id') ?? '';
  }
  set projectId(v: string) {
    if (v) this.setAttribute('project-id', v);
    else this.removeAttribute('project-id');
  }

  get labels(): Labels {
    const raw = this.getAttribute('labels');
    if (!raw) return DEFAULT_LABELS;
    try {
      return { ...DEFAULT_LABELS, ...(JSON.parse(raw) as Partial<Labels>) };
    } catch {
      return DEFAULT_LABELS;
    }
  }
  set labels(v: string | Partial<Labels>) {
    const json = typeof v === 'string' ? v : JSON.stringify(v);
    this.setAttribute('labels', json);
  }

  get theme(): 'light' | 'dark' | 'auto' {
    const v = this.getAttribute('theme');
    return v === 'light' || v === 'dark' ? v : 'auto';
  }
  set theme(v: 'light' | 'dark' | 'auto' | null) {
    if (v && v !== 'auto') this.setAttribute('theme', v);
    else this.removeAttribute('theme');
  }

  get placement(): Placement {
    const v = this.getAttribute('placement');
    return v === 'bottom-right' || v === 'inline' ? v : 'bottom-left';
  }
  set placement(v: Placement | null) {
    if (v && v !== 'bottom-left') this.setAttribute('placement', v);
    else this.removeAttribute('placement');
  }

  constructor() {
    super();
    this.root = this.attachShadow({ mode: 'open' });
  }

  connectedCallback(): void {
    const presetEmail = this.getAttribute('user-email');
    if (presetEmail && !this.userIdentifier) this.userIdentifier = presetEmail;
    this.applyTheme();
    this.setupThemeWatchers();
    this.render();
    document.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    document.removeEventListener('keydown', this.onKey);
    this.themeObserver?.disconnect();
    this.themeObserver = null;
    this.mqlDark?.removeEventListener('change', this.applyTheme);
    this.mqlDark = null;
  }

  attributeChangedCallback(name: string): void {
    if (name === 'theme') {
      this.applyTheme();
      return;
    }
    if (name === 'user-email') {
      const v = this.getAttribute('user-email');
      // Only adopt the attribute as long as the visitor hasn't typed their own.
      if (v && !this.emailTouched) this.userIdentifier = v;
    }
    if (this.root.firstChild) this.render();
  }

  private detectHostTheme(): 'light' | 'dark' {
    const prop = this.getAttribute('theme');
    if (prop === 'light' || prop === 'dark') return prop;

    if (typeof document === 'undefined') return 'light';
    const html = document.documentElement;
    const body = document.body;

    // Tailwind / shadcn / HeadlessUI / Radix Themes / daisyUI / Nuxt
    if (html.classList.contains('dark') || body?.classList.contains('dark')) return 'dark';

    // Mantine
    const mantine = html.getAttribute('data-mantine-color-scheme');
    if (mantine === 'dark') return 'dark';
    if (mantine === 'light') return 'light';

    // Bootstrap 5.3+, daisyUI, Chakra, generic data-theme
    const dt = html.getAttribute('data-theme') || html.getAttribute('data-bs-theme');
    if (dt) {
      if (/dark|night|black/i.test(dt)) return 'dark';
      if (/light|day/i.test(dt)) return 'light';
    }

    // System fallback
    return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches
      ? 'dark'
      : 'light';
  }

  private applyTheme = (): void => {
    const theme = this.detectHostTheme();
    if (this.getAttribute('data-theme') !== theme) {
      this.setAttribute('data-theme', theme);
    }
  };

  private setupThemeWatchers(): void {
    if (typeof window === 'undefined') return;
    this.themeObserver = new MutationObserver(() => this.applyTheme());
    this.themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class', 'data-theme', 'data-bs-theme', 'data-mantine-color-scheme'],
    });
    if (document.body) {
      this.themeObserver.observe(document.body, { attributes: true, attributeFilter: ['class'] });
    }
    this.mqlDark = matchMedia('(prefers-color-scheme: dark)');
    this.mqlDark.addEventListener('change', this.applyTheme);
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && this.open) {
      this.open = false;
      this.render();
    }
  };

  private async compressImage(
    source: Blob | HTMLCanvasElement,
    mime: 'image/jpeg' | 'image/webp' = 'image/jpeg',
    quality: number = COMPRESS_QUALITY,
    maxDim: number = COMPRESS_MAX_DIMENSION,
  ): Promise<Blob | null> {
    let width: number;
    let height: number;
    let drawSource: CanvasImageSource;
    let cleanup: (() => void) | null = null;

    if (source instanceof HTMLCanvasElement) {
      width = source.width;
      height = source.height;
      drawSource = source;
    } else {
      const url = URL.createObjectURL(source);
      cleanup = () => URL.revokeObjectURL(url);
      try {
        const img = await new Promise<HTMLImageElement>((resolve, reject) => {
          const i = new Image();
          i.onload = () => resolve(i);
          i.onerror = () => reject(new Error('image decode failed'));
          i.src = url;
        });
        width = img.naturalWidth;
        height = img.naturalHeight;
        drawSource = img;
      } catch (err) {
        cleanup();
        throw err;
      }
    }

    const ratio = Math.min(1, maxDim / Math.max(width, height));
    const targetW = Math.max(1, Math.round(width * ratio));
    const targetH = Math.max(1, Math.round(height * ratio));
    const canvas = document.createElement('canvas');
    canvas.width = targetW;
    canvas.height = targetH;
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      cleanup?.();
      return null;
    }
    // Flatten transparency to white for JPEG output.
    if (mime === 'image/jpeg') {
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, targetW, targetH);
    }
    ctx.drawImage(drawSource, 0, 0, targetW, targetH);
    cleanup?.();
    return await new Promise<Blob | null>((resolve) =>
      canvas.toBlob((b) => resolve(b), mime, quality),
    );
  }

  private async captureScreenshot(): Promise<void> {
    if (this.capturing) return;
    this.capturing = true;
    this.render();
    const prevVisibility = this.style.visibility;
    this.style.visibility = 'hidden';
    const restore = (): void => {
      if (prevVisibility) this.style.visibility = prevVisibility;
      else this.style.removeProperty('visibility');
    };
    try {
      const html2canvasMod = await import('html2canvas-pro');
      const html2canvas = html2canvasMod.default;
      await new Promise((r) => requestAnimationFrame(() => r(null)));
      const capturePromise = html2canvas(document.body, {
        backgroundColor: null,
        useCORS: true,
        logging: false,
        scale: Math.min(window.devicePixelRatio || 1, 2),
      });
      const timeoutPromise = new Promise<never>((_, reject) => {
        setTimeout(() => reject(new Error('capture timeout')), 15000);
      });
      const canvas = await Promise.race([capturePromise, timeoutPromise]);
      const blob = await this.compressImage(canvas, 'image/jpeg');
      if (!blob) throw new Error('capture failed');
      this.addAttachment(blob, 'screenshot.jpg');
    } catch (err) {
      this.errorMsg = err instanceof Error ? err.message : 'capture failed';
      this.state = 'error';
    } finally {
      restore();
      this.capturing = false;
      this.render();
    }
  }

  private addAttachment(blob: Blob, name: string): void {
    if (this.attachments.length >= MAX_ATTACHMENTS) {
      this.errorMsg = `Max ${MAX_ATTACHMENTS} pieces jointes`;
      this.state = 'error';
      return;
    }
    if (!ALLOWED_MIME.includes(blob.type)) {
      this.errorMsg = 'Type de fichier non supporté';
      this.state = 'error';
      return;
    }
    if (blob.size > MAX_ATTACHMENT_BYTES) {
      this.errorMsg = `Fichier trop volumineux (max ${Math.round(MAX_ATTACHMENT_BYTES / 1024 / 1024)} Mo)`;
      this.state = 'error';
      return;
    }
    const previewUrl = URL.createObjectURL(blob);
    this.attachments.push({ blob, previewUrl, name });
  }

  private removeAttachment(index: number): void {
    const att = this.attachments[index];
    if (att) URL.revokeObjectURL(att.previewUrl);
    this.attachments.splice(index, 1);
    this.render();
  }

  private clearAttachments(): void {
    for (const a of this.attachments) URL.revokeObjectURL(a.previewUrl);
    this.attachments = [];
  }

  private async onFilesPicked(files: FileList | null): Promise<void> {
    if (!files) return;
    for (const f of Array.from(files)) {
      let blob: Blob = f;
      let name = f.name;
      const isCompressible =
        f.type === 'image/png' || f.type === 'image/jpeg' || f.type === 'image/webp';
      if (isCompressible && f.size > COMPRESS_THRESHOLD_BYTES) {
        try {
          const compressed = await this.compressImage(f, 'image/jpeg');
          if (compressed && compressed.size < f.size) {
            blob = compressed;
            name = name.replace(/\.(png|webp|jpe?g)$/i, '') + '.jpg';
          }
        } catch {
          // fall through with the original blob; addAttachment will reject if too large
        }
      }
      this.addAttachment(blob, name);
    }
    this.render();
  }

  private async submit(): Promise<void> {
    if (!this.message.trim() || !this.endpoint || !this.projectId) {
      this.state = 'error';
      this.errorMsg = 'widget misconfigured';
      this.render();
      return;
    }
    this.state = 'loading';
    this.render();
    try {
      const url = `${this.endpoint.replace(/\/$/, '')}/api/projects/${encodeURIComponent(this.projectId)}/feedback`;
      const form = new FormData();
      form.append('type', this.type);
      form.append('message', this.message);
      form.append('url', location.pathname);
      form.append('device', window.innerWidth < 768 ? 'mobile' : 'desktop');
      form.append('user_identifier', this.userIdentifier);
      for (const a of this.attachments) {
        form.append('attachments', a.blob, a.name);
      }
      const res = await fetch(url, {
        method: 'POST',
        headers: this.apiKey ? { 'x-api-key': this.apiKey } : {},
        body: form,
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      this.state = 'sent';
      this.message = '';
      this.type = 'other';
      this.emailTouched = false;
      this.clearAttachments();
      this.render();
      this.dispatchEvent(new CustomEvent('skalpai-feedback-sent'));
      setTimeout(() => {
        this.open = false;
        this.state = 'idle';
        this.render();
      }, 1500);
    } catch (err) {
      this.state = 'error';
      this.errorMsg = err instanceof Error ? err.message : 'Error';
      this.render();
    }
  }

  private render(): void {
    const L = this.labels;
    const device = window.innerWidth < 768 ? 'mobile' : 'desktop';

    this.root.innerHTML = `
      <style>${STYLES}</style>
      <button class="btn-fab" type="button" aria-label="${L.title}">
        <span aria-hidden="true">💬</span><span>${L.title}</span>
      </button>
      ${
        this.open
          ? `
        <div class="panel" role="dialog" aria-label="${L.title}">
          <div class="head">
            <span>${L.title}</span>
            <button class="close" type="button" aria-label="${L.close}">×</button>
          </div>
          ${
            this.state === 'sent'
              ? `<div class="ok">${L.thanks}</div>`
              : `
            <form class="body">
              ${this.state === 'error' ? `<div class="err">${escapeHtml(this.errorMsg)}</div>` : ''}
              <div class="types">
                ${(['bug', 'idea', 'other'] as FeedbackType[])
                  .map(
                    (t) => `
                  <button class="type" type="button" data-type="${t}" aria-pressed="${this.type === t}">${L[t]}</button>
                `,
                  )
                  .join('')}
              </div>
              <input class="email" type="email" inputmode="email" autocomplete="email"
                placeholder="${L.email_placeholder}" value="${escapeHtml(this.userIdentifier)}" />
              ${
                this.emailTouched && this.userIdentifier.trim() && !EMAIL_RE.test(this.userIdentifier.trim())
                  ? `<div class="email-hint">${L.email_invalid}</div>`
                  : ''
              }
              <textarea placeholder="${L.placeholder}" rows="3">${escapeHtml(this.message)}</textarea>
              <div class="capture-row">
                <button class="capture-btn" type="button" ${this.capturing ? 'disabled' : ''} aria-label="${L.capture}">
                  <span aria-hidden="true">📷</span>
                  <span>${this.capturing ? L.capturing : L.capture}</span>
                </button>
                <button class="capture-btn attach-btn" type="button" aria-label="${L.attach}" ${this.attachments.length >= MAX_ATTACHMENTS ? 'disabled' : ''}>
                  <span aria-hidden="true">📎</span>
                  <span>${L.attach}</span>
                </button>
                <input class="file-input" type="file" accept="image/png,image/jpeg,image/webp,image/gif" multiple hidden />
              </div>
              ${
                this.attachments.length
                  ? `<div class="attach-list">
                      ${this.attachments
                        .map(
                          (a, i) => `<div class="preview">
                            <img src="${a.previewUrl}" alt="attachment ${i + 1}" />
                            <button class="preview-remove" type="button" data-index="${i}" aria-label="${L.remove_attachment}">×</button>
                          </div>`,
                        )
                        .join('')}
                    </div>`
                  : ''
              }
              <div class="meta">${device} · ${escapeHtml(location.pathname)}</div>
              <button class="submit" type="submit" ${
                this.state === 'loading' || !this.message.trim() ? 'disabled' : ''
              }>
                ${this.state === 'loading' ? L.sending : L.send}
              </button>
            </form>
          `
          }
        </div>
      `
          : ''
      }
    `;

    this.root.querySelector('.btn-fab')?.addEventListener('click', () => {
      this.open = !this.open;
      this.render();
    });
    this.root.querySelector('.close')?.addEventListener('click', () => {
      this.open = false;
      this.render();
    });
    this.root.querySelectorAll<HTMLButtonElement>('.type').forEach((btn) => {
      btn.addEventListener('click', () => {
        this.type = btn.dataset.type as FeedbackType;
        this.render();
      });
    });
    const emailInput = this.root.querySelector<HTMLInputElement>('.email');
    if (emailInput) {
      emailInput.addEventListener('input', (e) => {
        this.userIdentifier = (e.target as HTMLInputElement).value;
        this.emailTouched = true;
      });
      // Re-render on blur so the invalid hint can appear without stealing focus
      // mid-typing.
      emailInput.addEventListener('blur', () => {
        if (this.emailTouched) this.render();
      });
    }
    const ta = this.root.querySelector('textarea');
    if (ta) {
      ta.addEventListener('input', (e) => {
        this.message = (e.target as HTMLTextAreaElement).value;
        const submit = this.root.querySelector<HTMLButtonElement>('.submit');
        if (submit) submit.disabled = !this.message.trim() || this.state === 'loading';
      });
      if (this.open && this.state === 'idle') ta.focus();
    }
    this.root.querySelector('form')?.addEventListener('submit', (e) => {
      e.preventDefault();
      void this.submit();
    });
    this.root
      .querySelector<HTMLButtonElement>('.capture-btn:not(.attach-btn)')
      ?.addEventListener('click', () => {
        void this.captureScreenshot();
      });
    const fileInput = this.root.querySelector<HTMLInputElement>('.file-input');
    this.root.querySelector<HTMLButtonElement>('.attach-btn')?.addEventListener('click', () => {
      fileInput?.click();
    });
    fileInput?.addEventListener('change', (e) => {
      const target = e.target as HTMLInputElement;
      this.onFilesPicked(target.files);
      target.value = '';
    });
    this.root.querySelectorAll<HTMLButtonElement>('.preview-remove').forEach((btn) => {
      btn.addEventListener('click', () => {
        const i = Number(btn.dataset.index);
        if (!Number.isNaN(i)) this.removeAttachment(i);
      });
    });
  }
}

if (
  typeof window !== 'undefined' &&
  typeof customElements !== 'undefined' &&
  !customElements.get('skalpai-feedback')
) {
  customElements.define('skalpai-feedback', SkalpaiFeedbackElement);
}

declare global {
  interface HTMLElementTagNameMap {
    'skalpai-feedback': SkalpaiFeedbackElement;
  }
}
