import { describe, expect, it } from "vitest";

import { assemble, segments, surroundings } from "./diff";

describe("segments", () => {
  it("keeps unchanged text and isolates each change", () => {
    const segs = segments("Docker finit vite. Le reste tient.", "Docker termine vite. Le reste tient.");
    expect(segs).toEqual([
      { kind: "same", text: "Docker " },
      { kind: "change", id: "c0", before: "finit", after: "termine" },
      { kind: "same", text: " vite. Le reste tient." },
    ]);
  });

  it("joins two edits a single word apart into one passage", () => {
    const changes = segments("un petit chat noir", "un grand chat blanc").filter((s) => s.kind === "change");
    expect(changes).toEqual([{ kind: "change", id: "c0", before: "petit chat noir", after: "grand chat blanc" }]);
  });

  it("rebuilds both texts exactly", () => {
    const before = "# Titre\n\nUn texte long.\n\n- un\n- deux\n\n| a | b |";
    const after = "# Nouveau titre\n\nUn texte court et net.\n\n- un\n- trois\n\n| a | b |";
    const segs = segments(before, after);
    const ids = segs.flatMap((s) => (s.kind === "change" ? [s.id] : []));
    const all = (status: "accepted" | "rejected") => Object.fromEntries(ids.map((id) => [id, { status }]));
    expect(assemble(segs, all("rejected"))).toBe(before);
    expect(assemble(segs, all("accepted"))).toBe(after);
  });

  it("returns nothing to decide when nothing changed", () => {
    expect(segments("pareil", "pareil")).toEqual([{ kind: "same", text: "pareil" }]);
  });
});

describe("assemble", () => {
  it("mixes decisions and takes a rework over the proposal", () => {
    const segs = segments("A un. Puis B deux. Puis C trois.", "A one. Puis B two. Puis C three.");
    const ids = segs.flatMap((s) => (s.kind === "change" ? [s.id] : []));
    expect(ids.length).toBe(3);
    const text = assemble(
      segs,
      { [ids[0]]: { status: "accepted" }, [ids[1]]: { status: "rejected" }, [ids[2]]: { status: "accepted" } },
      { [ids[2]]: "trois et demi." },
    );
    expect(text).toBe("A one. Puis B deux. Puis C trois et demi.");
  });

  it("keeps the original of anything left undecided", () => {
    const segs = segments("garde ça", "change ça");
    expect(assemble(segs, {})).toBe("garde ça");
  });
});

describe("surroundings", () => {
  it("gives the text on each side of a change", () => {
    const segs = segments("avant X après", "avant Y après");
    const id = segs.find((s) => s.kind === "change")!;
    expect(surroundings(segs, (id as { id: string }).id)).toEqual({ before: "avant ", after: " après" });
  });
});

describe("segments by line", () => {
  it("keeps whole lines and parts runs only on a shared line with words", () => {
    const segs = segments("a\nb\nmilieu\nc\n", "A\nB\nmilieu\nC\n", "c", "line");
    expect(segs).toEqual([
      { kind: "change", id: "c0", before: "a\nb\n", after: "A\nB\n" },
      { kind: "same", text: "milieu\n" },
      { kind: "change", id: "c1", before: "c\n", after: "C\n" },
    ]);
  });
});
