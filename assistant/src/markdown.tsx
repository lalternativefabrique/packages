import type { ReactNode } from "react";

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(\[[^\]]+\]\([^)\s]+\))/g;

function safeHref(url: string): string | undefined {
  return /^https?:\/\//i.test(url) ? url : undefined;
}

function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let i = 0;
  for (const m of text.matchAll(INLINE)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const token = m[0];
    const k = `${key}-${i++}`;
    if (token.startsWith("`")) out.push(<code key={k}>{token.slice(1, -1)}</code>);
    else if (token.startsWith("**")) out.push(<strong key={k}>{inline(token.slice(2, -2), k)}</strong>);
    else if (token.startsWith("*")) out.push(<em key={k}>{inline(token.slice(1, -1), k)}</em>);
    else {
      const label = token.slice(1, token.indexOf("]("));
      const href = safeHref(token.slice(token.indexOf("](") + 2, -1));
      out.push(
        href ? (
          <a key={k} href={href} target="_blank" rel="noreferrer noopener">
            {label}
          </a>
        ) : (
          label
        ),
      );
    }
    last = at + token.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// A small renderer for hosts without a markdown library: paragraphs, headings,
// lists, fenced code, inline code, bold, italic and http(s) links. It builds
// React nodes only, so model output never reaches the DOM as HTML.
export function SafeMarkdown({ text }: { text: string }) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let n = 0;
  while (i < lines.length) {
    const line = lines[i];
    const key = `b${n++}`;
    if (line.startsWith("```")) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) code.push(lines[i++]);
      i++;
      blocks.push(
        <pre key={key}>
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      blocks.push(
        <p key={key} className="lalt-assistant-md-heading">
          <strong>{inline(heading[2], key)}</strong>
        </p>,
      );
      i++;
      continue;
    }
    const bullet = /^\s*[-*]\s+/;
    const ordered = /^\s*\d+[.)]\s+/;
    if (bullet.test(line) || ordered.test(line)) {
      const isOrdered = ordered.test(line);
      const pattern = isOrdered ? ordered : bullet;
      const items: ReactNode[] = [];
      while (i < lines.length && pattern.test(lines[i])) {
        items.push(<li key={`${key}-${items.length}`}>{inline(lines[i].replace(pattern, ""), `${key}-${items.length}`)}</li>);
        i++;
      }
      blocks.push(isOrdered ? <ol key={key}>{items}</ol> : <ul key={key}>{items}</ul>);
      continue;
    }
    if (!line.trim()) {
      i++;
      continue;
    }
    const para: string[] = [];
    while (i < lines.length && lines[i].trim() && !lines[i].startsWith("```") && !/^#{1,6}\s/.test(lines[i]) && !bullet.test(lines[i]) && !ordered.test(lines[i])) {
      para.push(lines[i++]);
    }
    blocks.push(
      <p key={key}>
        {para.flatMap((l, j) => (j === 0 ? inline(l, `${key}-${j}`) : [<br key={`${key}-br${j}`} />, ...inline(l, `${key}-${j}`)]))}
      </p>,
    );
  }
  return <div className="lalt-assistant-md">{blocks}</div>;
}
