import './index.js';

const script = document.currentScript as HTMLScriptElement | null;
if (script && typeof document !== 'undefined') {
  const el = document.createElement('skalpai-feedback');
  const apiKey = script.dataset.apiKey;
  const endpoint = script.dataset.endpoint;
  const projectId = script.dataset.projectId;
  const labels = script.dataset.labels;
  const userEmail = script.dataset.userEmail;
  const placement = script.dataset.placement;
  const collapsed = script.dataset.collapsed;
  if (apiKey) el.setAttribute('api-key', apiKey);
  if (endpoint) el.setAttribute('endpoint', endpoint);
  if (projectId) el.setAttribute('project-id', projectId);
  if (labels) el.setAttribute('labels', labels);
  if (userEmail) el.setAttribute('user-email', userEmail);
  if (placement) el.setAttribute('placement', placement);
  if (collapsed === 'true' || collapsed === '') el.setAttribute('collapsed', '');

  if (document.body) {
    document.body.appendChild(el);
  } else {
    document.addEventListener('DOMContentLoaded', () => document.body.appendChild(el), { once: true });
  }
}
