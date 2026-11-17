import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { codeRows } from "./code";
import { ReviewCode } from "./review-code";

const lines = (n: number, from = 1) => Array.from({ length: n }, (_, i) => `line ${from + i}\n`).join("");
const before = lines(20);
const after = lines(9) + "changed 10\n" + lines(10, 11);

describe("codeRows", () => {
  it("keeps context around a change and folds the long unchanged runs", () => {
    const rows = codeRows(before, after, 2);
    expect(rows.map((r) => r.kind)).toEqual(["fold", "same", "same", "change", "same", "same", "fold"]);
    const change = rows[3];
    expect(change).toMatchObject({ kind: "change", oldFrom: 10, newFrom: 10, oldCount: 1, newCount: 1 });
    expect(rows[1]).toMatchObject({ kind: "same", old: 8, new: 8, text: "line 8" });
    const tail = rows[6];
    expect(tail.kind === "fold" && tail.rows.length).toBe(8);
  });

  it("numbers both sides apart once lines are added", () => {
    const rows = codeRows("a\nb\nc\n", "a\nnew\nb\nc\n", 3);
    const c = rows.find((r) => r.kind === "same" && r.text === "c");
    expect(c).toMatchObject({ old: 3, new: 4 });
  });

  it("puts each note after the line it is about, even inside a fold", () => {
    const rows = codeRows(before, after, 1, [
      { line: 10, text: "why ten" },
      { line: 18, text: "far below" },
    ]);
    const kinds = rows.map((r) => (r.kind === "note" ? `note:${r.note.text}` : r.kind));
    expect(kinds.indexOf("note:why ten")).toBe(kinds.indexOf("change") + 1);
    expect(kinds.at(-1)).toBe("note:far below");
  });
});

describe("ReviewCode", () => {
  afterEach(cleanup);

  it("unfolds hidden lines on demand", () => {
    render(<ReviewCode before={before} after={after} context={1} />);
    expect(screen.queryByText("line 3")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /8 lignes inchangées/ }));
    expect(screen.getByText("line 3")).toBeTruthy();
  });

  it("sends a comment on one change and shows it as sent", async () => {
    const onComment = vi.fn(() => Promise.resolve());
    render(<ReviewCode before={before} after={after} onComment={onComment} />);
    fireEvent.click(screen.getByRole("button", { name: "Commenter" }));
    fireEvent.change(screen.getByLabelText("Commenter"), { target: { value: "keep the old name" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Envoyer" }));
    });
    expect(onComment).toHaveBeenCalledWith(
      expect.objectContaining({ line: 10, before: "line 10\n", after: "changed 10\n", text: "keep the old name" }),
    );
    expect(screen.getByText("keep the old name")).toBeTruthy();
  });

  it("says so when nothing changed", () => {
    render(<ReviewCode before="same" after="same" />);
    expect(screen.getByText("Aucun changement.")).toBeTruthy();
  });
});
