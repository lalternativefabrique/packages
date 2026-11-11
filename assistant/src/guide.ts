import type { AssistantGuideStep, AssistantScreenField } from "./types";

const FIELD_ATTR = "data-assistant-field";
const HOST_ID = "lalt-assistant-guide";
const MAX_FIELDS = 80;
const MAX_STEPS = 12;
const SECRET = /password|cc-|card|iban|cvc|cvv|one-time-code/i;

export interface GuideLabels {
  title: string;
  previous: string;
  next: string;
  done: string;
}

const defaultGuideLabels: GuideLabels = {
  title: "Guide de l'assistant",
  previous: "Précédent",
  next: "Suivant",
  done: "J'ai compris",
};

function visible(el: HTMLElement): boolean {
  const r = el.getBoundingClientRect();
  if (r.width < 4 || r.height < 4) return false;
  const style = getComputedStyle(el);
  if (style.visibility === "hidden" || style.display === "none") return false;
  return !el.closest('[aria-hidden="true"], [hidden], [inert], .lalt-assistant, [data-assistant-ignore], #' + HOST_ID);
}

function labelOf(el: HTMLElement): string {
  const input = el as HTMLInputElement;
  const fromLabel = input.labels?.[0]?.textContent;
  const byIds = (el.getAttribute("aria-labelledby") ?? "")
    .split(/\s+/)
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? "")
    .join(" ");
  return (fromLabel || el.getAttribute("aria-label") || byIds || input.placeholder || input.name || "")
    .trim()
    .replace(/\s+/g, " ")
    .slice(0, 120);
}

export function readScreenFields(root: ParentNode = document): AssistantScreenField[] {
  const out: AssistantScreenField[] = [];
  const nodes = root.querySelectorAll<HTMLElement>(
    'input, select, textarea, [contenteditable="true"], [contenteditable=""], [role="textbox"], [role="combobox"]',
  );
  for (const el of nodes) {
    if (out.length >= MAX_FIELDS) break;
    const input = el as HTMLInputElement;
    const type = (input.type || el.getAttribute("role") || (el.isContentEditable ? "richtext" : el.tagName)).toLowerCase();
    if (["hidden", "submit", "button", "reset", "image", "file"].includes(type) || input.disabled) continue;
    if (!visible(el)) continue;
    const ref = String(out.length);
    el.setAttribute(FIELD_ATTR, ref);
    const label = labelOf(el);
    const secret = type === "password" || SECRET.test(`${input.autocomplete ?? ""} ${input.name ?? ""} ${label}`);
    const field: AssistantScreenField = { ref, label, type };
    if (input.required || el.getAttribute("aria-required") === "true") field.required = true;
    if (secret) field.protected = true;
    else if (type === "checkbox" || type === "radio") field.value = input.checked ? "true" : "false";
    else field.value = ("value" in input ? input.value : el.textContent ?? "").slice(0, 200);
    if (el instanceof HTMLSelectElement) field.options = Array.from(el.options, (o) => o.text.trim()).slice(0, 50);
    out.push(field);
  }
  return out;
}

type GuideHost = HTMLDivElement & { __close?: () => void };

export function clearGuide(): void {
  const host = document.getElementById(HOST_ID) as GuideHost | null;
  if (host?.__close) host.__close();
  else host?.remove();
}

const STYLE = `
:host{all:initial}
svg{position:fixed;inset:0;width:100vw;height:100vh}
.ring{position:fixed;border:3px solid #3b82f6;border-radius:10px;box-shadow:0 0 0 4px rgba(59,130,246,.25);transition:border-color .3s}
.ring.done{border-color:#16a34a}
.ring.current{animation:pulse 1.6s ease-in-out infinite}
@keyframes pulse{50%{box-shadow:0 0 0 9px rgba(59,130,246,.18)}}
@media (prefers-reduced-motion:reduce){.ring.current{animation:none}}
.tip{position:fixed;max-width:280px;background:#0f172a;color:#f8fafc;font:14px/1.4 system-ui,sans-serif;padding:8px 10px 8px 34px;border-radius:8px;box-shadow:0 6px 20px rgba(0,0,0,.3)}
.n{position:absolute;left:8px;top:8px;width:20px;height:20px;border-radius:50%;background:#3b82f6;color:#fff;font:600 12px/20px system-ui;text-align:center}
.tip.done .n{background:#16a34a}
.bar{position:fixed;left:50%;bottom:20px;transform:translateX(-50%);pointer-events:auto;display:flex;gap:8px;align-items:center;background:#0f172a;color:#f8fafc;font:14px system-ui,sans-serif;padding:8px 8px 8px 14px;border-radius:999px;box-shadow:0 6px 20px rgba(0,0,0,.35)}
button{all:unset;cursor:pointer;padding:6px 12px;border-radius:999px;background:#1e293b;color:#f8fafc}
button.primary{background:#3b82f6}
button:focus-visible{outline:2px solid #93c5fd}
`;

interface Placed {
  step: AssistantGuideStep;
  el: HTMLElement;
  ring: HTMLDivElement;
  tip: HTMLDivElement;
  hole: SVGRectElement;
  mark: () => void;
}

export function showGuide(
  steps: AssistantGuideStep[],
  labels: Partial<GuideLabels> = {},
): { shown: string[]; missing: string[]; close: () => void } {
  clearGuide();
  const l = { ...defaultGuideLabels, ...labels };
  const missing: string[] = [];
  const found: { step: AssistantGuideStep; el: HTMLElement }[] = [];
  for (const step of steps.slice(0, MAX_STEPS)) {
    const el = /^\d+$/.test(step.field) ? document.querySelector<HTMLElement>(`[${FIELD_ATTR}="${step.field}"]`) : null;
    if (el) found.push({ step, el });
    else missing.push(step.field);
  }
  const noop = { shown: [], missing, close: () => {} };
  if (!found.length) return noop;

  const host = document.createElement("div") as GuideHost;
  host.id = HOST_ID;
  host.style.cssText = "position:fixed;inset:0;z-index:2147483647;pointer-events:none";
  const root = host.attachShadow({ mode: "closed" });
  const style = document.createElement("style");
  style.textContent = STYLE;
  const svgNs = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNs, "svg");
  const mask = document.createElementNS(svgNs, "mask");
  mask.id = "m";
  const maskBase = document.createElementNS(svgNs, "rect");
  for (const [k, v] of Object.entries({ width: "100%", height: "100%", fill: "#fff" })) maskBase.setAttribute(k, v);
  mask.append(maskBase);
  const defs = document.createElementNS(svgNs, "defs");
  defs.append(mask);
  const dim = document.createElementNS(svgNs, "rect");
  for (const [k, v] of Object.entries({ width: "100%", height: "100%", fill: "rgba(15,23,42,.45)", mask: "url(#m)" }))
    dim.setAttribute(k, v);
  svg.append(defs, dim);

  const layer = document.createElement("div");
  const placed: Placed[] = found.map(({ step, el }, i) => {
    const hole = document.createElementNS(svgNs, "rect");
    hole.setAttribute("rx", "10");
    hole.setAttribute("fill", "#000");
    mask.append(hole);
    const ring = document.createElement("div");
    ring.className = "ring";
    const tip = document.createElement("div");
    tip.className = "tip";
    const n = document.createElement("span");
    n.className = "n";
    n.textContent = String(i + 1);
    tip.append(n, document.createTextNode(step.note));
    layer.append(ring, tip);
    const mark = () => {
      ring.classList.add("done");
      tip.classList.add("done");
    };
    el.addEventListener("input", mark);
    el.addEventListener("change", mark);
    return { step, el, ring, tip, hole, mark };
  });

  const bar = document.createElement("div");
  bar.className = "bar";
  bar.setAttribute("role", "dialog");
  bar.setAttribute("aria-label", l.title);
  const count = document.createElement("span");
  const button = (text: string, cls = "") => {
    const b = document.createElement("button");
    b.textContent = text;
    if (cls) b.className = cls;
    return b;
  };
  const prev = button(l.previous);
  const next = button(l.next, "primary");
  const done = button(l.done);
  bar.append(count, prev, next, done);
  root.append(style, svg, layer, bar);

  let current = 0;
  const place = () => {
    placed.forEach((p, i) => {
      const r = p.el.getBoundingClientRect();
      const pad = 6;
      const x = r.left - pad;
      const y = r.top - pad;
      const w = r.width + pad * 2;
      const h = r.height + pad * 2;
      for (const [k, v] of Object.entries({ x, y, width: w, height: h })) p.hole.setAttribute(k, String(v));
      Object.assign(p.ring.style, { left: `${x}px`, top: `${y}px`, width: `${w - 6}px`, height: `${h - 6}px` });
      p.ring.classList.toggle("current", i === current);
      const tw = Math.min(280, innerWidth - 16);
      p.tip.style.left = `${Math.max(8, Math.min(x, innerWidth - tw - 8))}px`;
      const below = y + h + 8;
      p.tip.style.top = `${below + 60 < innerHeight ? below : Math.max(8, y - p.tip.offsetHeight - 8)}px`;
      p.tip.style.opacity = i === current || placed.length <= 4 ? "1" : ".55";
    });
    count.textContent = `${current + 1} / ${placed.length}`;
    prev.style.display = current > 0 ? "" : "none";
    next.style.display = current < placed.length - 1 ? "" : "none";
  };
  const go = (i: number) => {
    current = Math.max(0, Math.min(placed.length - 1, i));
    placed[current].el.scrollIntoView?.({ block: "center", behavior: "smooth" });
    placed[current].el.focus({ preventScroll: true });
    place();
  };
  let frame = 0;
  const schedule = () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(place);
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") close();
  };
  const onFocus = placed.map((p, i) => () => {
    if (current !== i) {
      current = i;
      place();
    }
  });
  placed.forEach((p, i) => p.el.addEventListener("focus", onFocus[i]));
  const close = () => {
    cancelAnimationFrame(frame);
    removeEventListener("scroll", schedule, true);
    removeEventListener("resize", schedule);
    removeEventListener("keydown", onKey, true);
    placed.forEach((p, i) => {
      p.el.removeEventListener("input", p.mark);
      p.el.removeEventListener("change", p.mark);
      p.el.removeEventListener("focus", onFocus[i]);
    });
    host.remove();
  };
  host.__close = close;
  prev.addEventListener("click", () => go(current - 1));
  next.addEventListener("click", () => go(current + 1));
  done.addEventListener("click", close);
  addEventListener("scroll", schedule, true);
  addEventListener("resize", schedule);
  addEventListener("keydown", onKey, true);
  document.documentElement.append(host);
  go(0);
  return { shown: placed.map((p) => p.step.field), missing, close };
}
