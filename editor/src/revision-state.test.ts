import { describe, expect, it } from "vitest";

import {
  accept,
  acceptAll,
  openRevision,
  reject,
  rejectAll,
  resolved,
  resolvedBlocks,
} from "./revision-state";

const before = ["un", "deux", "trois"];
const after = ["un", "DEUX", "trois"];

describe("openRevision", () => {
  it("leaves every change pending", () => {
    const state = openRevision(before, after);

    expect(state.pending).toBe(1);
    expect(resolvedBlocks(state)).toEqual(before);
  });

  it("reports no pending change when the model returned the same text", () => {
    const state = openRevision(before, before);

    expect(state.pending).toBe(0);
  });
});

describe("accept", () => {
  it("applies one change and leaves the others alone", () => {
    const state = accept(openRevision(before, ["ZERO", "DEUX", "trois"]), "c0");

    expect(resolvedBlocks(state)).toEqual(["ZERO", "deux", "trois"]);
  });

  it("is idempotent", () => {
    const once = accept(openRevision(before, after), "c1");
    const twice = accept(once, "c1");

    expect(resolvedBlocks(twice)).toEqual(resolvedBlocks(once));
    expect(twice.pending).toBe(once.pending);
  });

  it("ignores an unknown id rather than throwing at the user", () => {
    const state = accept(openRevision(before, after), "nope");

    expect(state.pending).toBe(1);
  });
});

describe("reject", () => {
  it("keeps the original block", () => {
    const state = reject(openRevision(before, after), "c1");

    expect(resolvedBlocks(state)).toEqual(before);
    expect(state.pending).toBe(0);
  });

  it("drops an inserted block entirely", () => {
    const state = reject(openRevision(["un"], ["un", "deux"]), "c1");

    expect(resolvedBlocks(state)).toEqual(["un"]);
  });

  it("keeps a deleted block", () => {
    const state = reject(openRevision(["un", "deux"], ["un"]), "c1");

    expect(resolvedBlocks(state)).toEqual(["un", "deux"]);
  });
});

describe("acceptAll", () => {
  it("rebuilds the revision exactly", () => {
    const revised = ["zero", "un", "DEUX", "quatre"];

    expect(resolvedBlocks(acceptAll(openRevision(before, revised)))).toEqual(revised);
  });

  it("overrides changes already rejected one by one", () => {
    const state = acceptAll(reject(openRevision(before, after), "c1"));

    expect(resolvedBlocks(state)).toEqual(after);
    expect(state.pending).toBe(0);
  });
});

describe("rejectAll", () => {
  it("rebuilds the original exactly", () => {
    const state = rejectAll(openRevision(before, ["zero", "un", "DEUX"]));

    expect(resolvedBlocks(state)).toEqual(before);
    expect(state.pending).toBe(0);
  });
});

describe("resolved", () => {
  it("marks an accepted insertion as having no source block", () => {
    const state = acceptAll(openRevision(["un"], ["un", "deux"]));

    expect(resolved(state).map((r) => r.sourceIndex)).toEqual([0, -1]);
  });

  it("keeps later blocks pointing at their own source once an insertion shifts them", () => {
    // Without this the caller restores block shapes by position and every
    // heading after an accepted insertion turns into the wrong kind of node.
    const state = acceptAll(openRevision(["un", "deux"], ["zero", "un", "deux"]));

    expect(resolved(state).map((r) => r.sourceIndex)).toEqual([-1, 0, 1]);
  });

  it("keeps counting source blocks past a rejected deletion", () => {
    const state = rejectAll(openRevision(["un", "deux", "trois"], ["un", "trois"]));

    expect(resolved(state).map((r) => r.sourceIndex)).toEqual([0, 1, 2]);
  });
});

describe("mixed decisions", () => {
  it("accepts one change and rejects another independently", () => {
    let state = openRevision(before, ["UN", "DEUX", "trois"]);
    state = accept(state, "c0");
    state = reject(state, "c1");

    expect(resolvedBlocks(state)).toEqual(["UN", "deux", "trois"]);
    expect(state.pending).toBe(0);
  });

  it("counts down as the reviewer decides", () => {
    let state = openRevision(before, ["UN", "DEUX", "trois"]);
    expect(state.pending).toBe(2);

    state = accept(state, "c0");
    expect(state.pending).toBe(1);

    state = reject(state, "c1");
    expect(state.pending).toBe(0);
  });
});
