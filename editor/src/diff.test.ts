import { describe, expect, it } from "vitest";

import { diffBlocks } from "./diff";

const blocks = (text: string) => text.split("\n").filter(Boolean);

describe("diffBlocks", () => {
  it("reports nothing when both sides are identical", () => {
    const before = blocks("un\ndeux\ntrois");

    const changes = diffBlocks(before, before);

    expect(changes.every((c) => c.kind === "keep")).toBe(true);
  });

  it("pairs a rewritten block instead of reporting a delete and an insert", () => {
    const changes = diffBlocks(blocks("un\ndeux\ntrois"), blocks("un\nDEUX\ntrois"));

    const edits = changes.filter((c) => c.kind !== "keep");
    expect(edits).toHaveLength(1);
    expect(edits[0]).toMatchObject({ kind: "replace", before: "deux", after: "DEUX" });
  });

  it("keeps untouched blocks out of the review", () => {
    const changes = diffBlocks(blocks("un\ndeux\ntrois"), blocks("un\nDEUX\ntrois"));

    expect(changes.filter((c) => c.kind === "keep").map((c) => c.after)).toEqual(["un", "trois"]);
  });

  it("reports an added block as an insert", () => {
    const changes = diffBlocks(blocks("un\ndeux"), blocks("un\nun et demi\ndeux"));

    const edits = changes.filter((c) => c.kind !== "keep");
    expect(edits).toHaveLength(1);
    expect(edits[0]).toMatchObject({ kind: "insert", after: "un et demi" });
    expect(edits[0].before).toBe("");
  });

  it("reports a removed block as a delete", () => {
    const changes = diffBlocks(blocks("un\ndeux\ntrois"), blocks("un\ntrois"));

    const edits = changes.filter((c) => c.kind !== "keep");
    expect(edits).toHaveLength(1);
    expect(edits[0]).toMatchObject({ kind: "delete", before: "deux" });
    expect(edits[0].after).toBe("");
  });

  it("treats a wholly different block as a replace, not a delete plus an insert", () => {
    // Reviewing one replacement is a single decision; reviewing a delete and an
    // insert that mean the same thing is two, and they can disagree.
    const changes = diffBlocks(blocks("un\nle chat dort\ntrois"), blocks("un\nle chien court\ntrois"));

    const edits = changes.filter((c) => c.kind !== "keep");
    expect(edits).toHaveLength(1);
    expect(edits[0].kind).toBe("replace");
  });

  it("numbers every change so a reviewer can accept one of them", () => {
    const changes = diffBlocks(blocks("un\ndeux\ntrois"), blocks("zero\nun\nDEUX"));

    const ids = changes.map((c) => c.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("survives an empty original", () => {
    const changes = diffBlocks([], blocks("un\ndeux"));

    expect(changes).toHaveLength(2);
    expect(changes.every((c) => c.kind === "insert")).toBe(true);
  });

  it("survives an emptied revision", () => {
    const changes = diffBlocks(blocks("un\ndeux"), []);

    expect(changes).toHaveLength(2);
    expect(changes.every((c) => c.kind === "delete")).toBe(true);
  });

  it("keeps the revised order so applying every change rebuilds the revision", () => {
    const after = blocks("zero\nun\nDEUX\nquatre");

    const changes = diffBlocks(blocks("un\ndeux\ntrois"), after);
    const rebuilt = changes
      .filter((c) => c.kind !== "delete")
      .map((c) => c.after);

    expect(rebuilt).toEqual(after);
  });

  it("rebuilds the original when every change is rejected", () => {
    const before = blocks("un\ndeux\ntrois");

    const changes = diffBlocks(before, blocks("zero\nun\nDEUX"));
    const rebuilt = changes
      .filter((c) => c.kind !== "insert")
      .map((c) => c.before);

    expect(rebuilt).toEqual(before);
  });
});
