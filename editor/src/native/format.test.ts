import { describe, expect, it } from "vitest";

import { applyFormat, formatActions } from "./format";

const action = (id: string) => formatActions().find((a) => a.id === id)!;

describe("formatActions", () => {
  it("takes the host's labels", () => {
    const labels = Object.fromEntries(formatActions().map((a) => [a.id, a.id.toUpperCase()]));
    expect(formatActions(labels as never)[0].label).toBe("BOLD");
  });
});

describe("applyFormat", () => {
  it("wraps the selection and keeps it selected", () => {
    const out = applyFormat("un mot ici", { start: 3, end: 6 }, action("bold"));
    expect(out.text).toBe("un **mot** ici");
    expect(out.text.slice(out.selection.start, out.selection.end)).toBe("mot");
  });

  it("drops the markers at the caret when nothing is selected", () => {
    expect(applyFormat("ab", { start: 1, end: 1 }, action("italic")).text).toBe("a**b");
  });

  it("prefixes the line holding the caret", () => {
    const out = applyFormat("premier\nsecond", { start: 10, end: 10 }, action("h2"));
    expect(out.text).toBe("premier\n## second");
    expect(out.selection).toEqual({ start: 13, end: 13 });
  });
});
