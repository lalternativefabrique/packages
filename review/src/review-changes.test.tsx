import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ReviewChanges } from "./review-changes";

const fields = [
  { id: "title", label: "Titre", before: "Docker finit", after: "Docker termine" },
  { id: "body", before: "Un texte long. Puis la suite reste. Fin ici.", after: "Un texte court. Puis la suite reste. Fin là." },
];

describe("ReviewChanges", () => {
  afterEach(cleanup);

  it("applies each passage as decided, the rest keeping the original", async () => {
    const onApply = vi.fn();
    render(<ReviewChanges fields={fields} granularity="word" onApply={onApply} />);
    expect(screen.getByText(/3 à décider/)).toBeTruthy();

    const accept = screen.getAllByRole("button", { name: "Accepter" });
    fireEvent.click(accept[0]);
    fireEvent.click(screen.getAllByRole("button", { name: "Rejeter" })[0]);
    fireEvent.click(screen.getByRole("button", { name: "Appliquer" }));

    expect(onApply).toHaveBeenCalledWith({
      title: "Docker termine",
      body: "Un texte long. Puis la suite reste. Fin ici.",
    });
  });

  it("reworks a passage without blocking the others, then applies the rework", async () => {
    let resolve!: (text: string) => void;
    const onRework = vi.fn(() => new Promise<string>((r) => (resolve = r)));
    const onApply = vi.fn();
    render(<ReviewChanges fields={[fields[1]]} granularity="word" onRework={onRework} onApply={onApply} />);

    fireEvent.click(screen.getAllByRole("button", { name: "Retravailler" })[0]);
    fireEvent.change(screen.getByPlaceholderText(/Ce qui ne va pas/), { target: { value: "plus précis" } });
    fireEvent.click(screen.getByRole("button", { name: "Envoyer" }));

    expect(onRework).toHaveBeenCalledWith(
      expect.objectContaining({ fieldId: "body", original: "long.", proposal: "court.", instruction: "plus précis" }),
    );
    expect(screen.getByText(/Retravail en cours/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Appliquer" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getAllByRole("button", { name: "Accepter" }).at(-1)!);

    await act(async () => resolve("bref et net."));
    expect(screen.getByText("Retravaillé")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Accepter" })[0]);
    fireEvent.click(screen.getByRole("button", { name: "Appliquer" }));
    expect(onApply).toHaveBeenCalledWith({ body: "Un texte bref et net. Puis la suite reste. Fin là." });
  });

  it("reports a failed rework on its passage", async () => {
    const onRework = vi.fn(() => Promise.reject(new Error("quota atteint")));
    render(<ReviewChanges fields={[fields[0]]} onRework={onRework} onApply={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "Retravailler" }));
    fireEvent.change(screen.getByPlaceholderText(/Ce qui ne va pas/), { target: { value: "x" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Envoyer" }));
    });
    expect(screen.getByText("quota atteint")).toBeTruthy();
  });

  it("accepts everything at once", () => {
    const onApply = vi.fn();
    render(<ReviewChanges fields={fields} onApply={onApply} />);
    fireEvent.click(screen.getByRole("button", { name: "Tout accepter" }));
    fireEvent.click(screen.getByRole("button", { name: "Appliquer" }));
    expect(onApply).toHaveBeenCalledWith({ title: "Docker termine", body: fields[1].after });
  });

  it("leaves out a field that does not change, and shows before and after side by side", () => {
    render(
      <ReviewChanges
        fields={[{ id: "title", label: "Titre", before: "", after: "" }, fields[1]]}
        onApply={() => {}}
      />,
    );
    expect(screen.queryByText("Titre")).toBeNull();
    expect(screen.getByText("Avant")).toBeTruthy();
    expect(screen.getByText("Après")).toBeTruthy();
  });

  it("cuts a rewrite into runs of changed lines, not scattered words", () => {
    const before = "le nombre de tool est impressionnant\n\nun nouveau Herdr magic\n\nencore et encore mais où on va\n\nFin commune.";
    const after = "Le nombre d'outils est impressionnant.\n\nUn nouveau chaque jour.\n\nEncore. Chacun creuse son puits.\n\nFin commune.";
    const onApply = vi.fn();
    render(<ReviewChanges fields={[{ id: "body", before, after }]} onApply={onApply} />);
    expect(screen.getByText(/1 à décider/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Accepter" }));
    fireEvent.click(screen.getByRole("button", { name: "Appliquer" }));
    expect(onApply).toHaveBeenCalledWith({ body: after });
  });
});
