import { describe, expect, it, vi } from "vitest";
import type { Editor } from "@tiptap/react";

import { defaultSlashItems, filterSlashItems, type SlashItem } from "./slash";

function fakeEditor() {
  const calls: string[] = [];
  const chain: Record<string, unknown> = {};
  const record = (name: string) => (...args: unknown[]) => {
    calls.push(args.length > 0 ? `${name}:${JSON.stringify(args[0])}` : name);
    return chain;
  };
  for (const name of [
    "focus",
    "deleteRange",
    "setNode",
    "toggleBulletList",
    "toggleOrderedList",
    "toggleBlockquote",
    "toggleCodeBlock",
    "setHorizontalRule",
  ]) {
    chain[name] = record(name);
  }
  chain.run = () => true;
  return { editor: { chain: () => chain } as unknown as Editor, calls };
}

describe("defaultSlashItems", () => {
  it("gives every item a stable id", () => {
    const ids = defaultSlashItems().map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("names items with the labels it is given", () => {
    const [first] = defaultSlashItems({
      labels: {
        heading1: "Heading 1",
        heading2: "Heading 2",
        bulletList: "Bullets",
        orderedList: "Numbers",
        blockquote: "Quote",
        codeBlock: "Code",
        horizontalRule: "Divider",
      },
    });
    expect(first.title).toBe("Heading 1");
  });

  it("consumes the typed query before running the command", () => {
    const { editor, calls } = fakeEditor();
    const heading = defaultSlashItems()[0];

    heading.command({ editor, range: { from: 1, to: 3 } });

    expect(calls[1]).toBe("deleteRange:{\"from\":1,\"to\":3}");
    expect(calls).toContain("setNode:\"heading\"");
  });

  it("leaves the description empty unless the host writes one", () => {
    const plain = defaultSlashItems()[0];
    const described = defaultSlashItems({
      descriptions: { heading1: "Grand titre" },
    })[0];

    expect(plain.description).toBeUndefined();
    expect(described.description).toBe("Grand titre");
  });
});

describe("filterSlashItems", () => {
  const items: SlashItem[] = [
    { id: "a", title: "Titre 1", command: vi.fn() },
    { id: "b", title: "Citation", command: vi.fn() },
  ];

  it("returns everything for a blank query", () => {
    expect(filterSlashItems(items, "   ")).toEqual(items);
  });

  it("matches a title regardless of case", () => {
    expect(filterSlashItems(items, "CITA").map((i) => i.id)).toEqual(["b"]);
  });

  it("returns nothing when no title matches", () => {
    expect(filterSlashItems(items, "zzz")).toEqual([]);
  });
});
