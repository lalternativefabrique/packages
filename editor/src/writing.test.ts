import { describe, expect, it } from "vitest";

import { canAssist, hostOf, normalizeInstruction, proposalChanges } from "./writing";

describe("canAssist", () => {
  it("refuses a passage too short to rework", () => {
    expect(canAssist("  trop court ")).toBe(false);
  });

  it("accepts a passage of a sentence", () => {
    expect(canAssist("Une phrase entière à reprendre.")).toBe(true);
  });
});

describe("normalizeInstruction", () => {
  it("trims the instruction", () => {
    expect(normalizeInstruction("  Raccourcis \n")).toBe("Raccourcis");
  });

  it("rejects a blank instruction", () => {
    expect(normalizeInstruction("   ")).toBeNull();
  });
});

describe("proposalChanges", () => {
  it("reports no change when the model answered the passage back", () => {
    expect(proposalChanges("Même texte.", " Même texte. ")).toEqual([]);
  });

  it("keeps one decision per changed paragraph", () => {
    expect(proposalChanges("A.\n\nB.", "A.\n\nC.")).toEqual([{ before: "B.", after: "C." }]);
  });
});

describe("hostOf", () => {
  it("drops the www so the host reads as a name", () => {
    expect(hostOf("https://www.ladepeche.fr/article")).toBe("ladepeche.fr");
  });

  it("returns the input untouched when it cannot be parsed", () => {
    expect(hostOf("pas-une-url")).toBe("pas-une-url");
  });
});
