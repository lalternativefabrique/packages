import type { RichNode } from "./document";
import { hostOf } from "./writing";

export interface Citation {
  index: number;
  title: string;
  href?: string | null;
}

export interface WebSource {
  title: string;
  url: string;
  excerpt?: string;
}

export interface ConversationTurn {
  role: "user" | "assistant";
  content: string;
  /** A turn where the assistant asked back instead of answering. */
  clarify?: boolean;
  citations?: Citation[];
}

function text(value: string, marks?: RichNode[]): RichNode {
  return marks ? { type: "text", text: value, marks } : { type: "text", text: value };
}

function paragraph(content: RichNode[]): RichNode {
  return content.length > 0 ? { type: "paragraph", content } : { type: "paragraph" };
}

function link(value: string, href: string): RichNode {
  return text(value, [{ type: "link", attrs: { href } }]);
}

const MARKER = /\[(\d+)\]/g;

export function linkMarkers(block: string, citations: Citation[]): RichNode[] {
  const hrefs = new Map(citations.filter((c) => c.href).map((c) => [c.index, c.href as string]));
  const nodes: RichNode[] = [];
  let last = 0;
  for (const match of block.matchAll(MARKER)) {
    const href = hrefs.get(Number(match[1]));
    if (!href) continue;
    const at = match.index ?? 0;
    if (at > last) nodes.push(text(block.slice(last, at)));
    nodes.push(link(match[0], href));
    last = at + match[0].length;
  }
  if (last < block.length) nodes.push(text(block.slice(last)));
  return nodes;
}

function prose(answer: string, citations: Citation[]): RichNode[] {
  return answer
    .split(/\n{2,}/)
    .map((block) => block.trim())
    .filter((block) => block !== "")
    .map((block) => paragraph(linkMarkers(block, citations)));
}

function sourcesList(citations: Citation[], sourcesLabel: string, numbered: boolean): RichNode[] {
  const linkable = citations.filter((c): c is Citation & { href: string } => !!c.href);
  if (linkable.length === 0) return [];
  return [
    paragraph([text(sourcesLabel, [{ type: "italic" }])]),
    {
      type: "bulletList",
      content: linkable.map((c) => {
        const title = c.title.trim() || c.href;
        return {
          type: "listItem",
          content: [paragraph([link(numbered ? `[${c.index}] ${title}` : title, c.href)])],
        };
      }),
    },
  ];
}

export function webSourceToNodes(source: WebSource): RichNode[] {
  const nodes = [paragraph([link(source.title.trim() || source.url, source.url)])];
  const excerpt = source.excerpt?.trim();
  if (excerpt) nodes.push(paragraph([text(excerpt)]));
  nodes.push(paragraph([text(hostOf(source.url), [{ type: "italic" }])]));
  return [{ type: "blockquote", content: nodes }];
}

export function answerToNodes(params: {
  question: string;
  answer: string;
  citations: Citation[];
  sourcesLabel: string;
}): RichNode[] {
  const nodes: RichNode[] = [];
  const question = params.question.trim();
  if (question) nodes.push({ type: "blockquote", content: [paragraph([text(question)])] });
  nodes.push(...prose(params.answer, params.citations));
  nodes.push(...sourcesList(params.citations, params.sourcesLabel, true));
  return nodes;
}

export function headingFromQuestion(question: string): string {
  const trimmed = question.trim().replace(/\s*[?!.]+\s*$/, "");
  if (!trimmed) return "";
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

export function conversationToNodes(params: {
  turns: ConversationTurn[];
  sourcesLabel: string;
}): RichNode[] {
  const nodes: RichNode[] = [];
  const sources: Citation[] = [];
  const seen = new Set<string>();
  let question = "";

  for (const turn of params.turns) {
    if (turn.role === "user") {
      question = turn.content;
      continue;
    }
    if (turn.clarify) continue;

    const heading = headingFromQuestion(question);
    if (heading) nodes.push({ type: "heading", attrs: { level: 3 }, content: [text(heading)] });
    nodes.push(...prose(turn.content, turn.citations ?? []));
    for (const citation of turn.citations ?? []) {
      if (!citation.href || seen.has(citation.href)) continue;
      seen.add(citation.href);
      sources.push(citation);
    }
    question = "";
  }

  // Each turn numbers its citations from 1, so the gathered list cannot keep them.
  nodes.push(...sourcesList(sources, params.sourcesLabel, false));
  return nodes;
}
