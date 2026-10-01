import { describe, expect, it } from "vitest";

import type { RichNode } from "./document";
import {
  answerToNodes,
  conversationToNodes,
  headingFromQuestion,
  linkMarkers,
  webSourceToNodes,
  type Citation,
  type ConversationTurn,
} from "./answer";

const citation = (over: Partial<Citation> = {}): Citation => ({
  index: 1,
  title: "Mélenchon et la dette",
  href: "/notes/1",
  ...over,
});

const at = (node: RichNode, ...path: number[]): RichNode =>
  path.reduce<RichNode>((n, i) => (n.content as RichNode[])[i], node);

describe("answerToNodes", () => {
  const sourcesLabel = "Sources";

  it("opens on the question as a blockquote", () => {
    const [first] = answerToNodes({ question: "que dit-il ?", answer: "Réponse.", citations: [], sourcesLabel });
    expect(first).toMatchObject({ type: "blockquote" });
  });

  it("splits the answer on blank lines, one paragraph each", () => {
    const nodes = answerToNodes({ question: "", answer: "Premier.\n\nSecond.", citations: [], sourcesLabel });
    expect(nodes).toHaveLength(2);
    expect(nodes.every((n) => n.type === "paragraph")).toBe(true);
  });

  it("lists each citation as a numbered link", () => {
    const nodes = answerToNodes({
      question: "",
      answer: "Réponse.",
      citations: [citation({ index: 2 })],
      sourcesLabel,
    });
    const list = nodes.find((n) => n.type === "bulletList") as RichNode;
    const label = at(list, 0, 0, 0);
    expect(label.text).toBe("[2] Mélenchon et la dette");
    expect((label.marks as RichNode[])[0]).toMatchObject({ type: "link", attrs: { href: "/notes/1" } });
  });

  it("links the markers inside the answer", () => {
    const [paragraph] = answerToNodes({
      question: "",
      answer: "La dette [1] reste.",
      citations: [citation()],
      sourcesLabel,
    });
    expect((paragraph.content as RichNode[]).map((n) => n.text)).toEqual(["La dette ", "[1]", " reste."]);
  });

  it("omits the sources list when nothing can be linked", () => {
    const nodes = answerToNodes({
      question: "",
      answer: "Réponse.",
      citations: [citation({ href: null })],
      sourcesLabel,
    });
    expect(nodes.some((n) => n.type === "bulletList")).toBe(false);
  });
});

describe("linkMarkers", () => {
  it("leaves a marker without a citation as text", () => {
    expect(linkMarkers("Voir [3].", [citation()])).toEqual([{ type: "text", text: "Voir [3]." }]);
  });
});

describe("webSourceToNodes", () => {
  it("carries the link on the title inside a blockquote", () => {
    const [node] = webSourceToNodes({ title: "Titre", url: "https://a.fr/x" });
    expect(node.type).toBe("blockquote");
    expect(at(node, 0, 0).text).toBe("Titre");
  });

  it("falls back to the url when a result has no title", () => {
    const [node] = webSourceToNodes({ title: "  ", url: "https://a.fr/x" });
    expect(at(node, 0, 0).text).toBe("https://a.fr/x");
  });

  it("omits the excerpt paragraph when there is none", () => {
    const [withExcerpt] = webSourceToNodes({ title: "T", url: "https://a.fr", excerpt: "e" });
    const [without] = webSourceToNodes({ title: "T", url: "https://a.fr" });
    expect(withExcerpt.content).toHaveLength(3);
    expect(without.content).toHaveLength(2);
  });
});

describe("headingFromQuestion", () => {
  it("drops the interrogative tail and capitalises", () => {
    expect(headingFromQuestion("peux tu en dire plus ?")).toBe("Peux tu en dire plus");
  });
});

describe("conversationToNodes", () => {
  const first = citation({ href: "/s1", title: "Première" });
  const second = citation({ index: 2, href: "/s2", title: "Seconde" });
  const turns: ConversationTurn[] = [
    { role: "user", content: "melenchon en 2027" },
    { role: "assistant", content: "Annuler une partie de la dette.", citations: [first] },
    { role: "assistant", content: "Précisez ?", clarify: true },
    { role: "user", content: "et son programme ?" },
    { role: "assistant", content: "Présentée en juin.", citations: [first, second] },
  ];

  it("keeps every answered turn and skips clarifications", () => {
    const flat = JSON.stringify(conversationToNodes({ turns, sourcesLabel: "Sources" }));
    expect(flat).toContain("Annuler une partie");
    expect(flat).toContain("Présentée en juin");
    expect(flat).not.toContain("Précisez");
  });

  it("turns each question into a heading", () => {
    const headings = conversationToNodes({ turns, sourcesLabel: "Sources" }).filter((n) => n.type === "heading");
    expect(headings.map((h) => at(h, 0).text)).toEqual(["Melenchon en 2027", "Et son programme"]);
  });

  it("gathers the sources once, deduplicated, at the end", () => {
    const nodes = conversationToNodes({ turns, sourcesLabel: "Sources" });
    const lists = nodes.filter((n) => n.type === "bulletList");
    expect(lists).toHaveLength(1);
    expect(lists[0].content).toHaveLength(2);
    expect(nodes[nodes.length - 1].type).toBe("bulletList");
  });
});
