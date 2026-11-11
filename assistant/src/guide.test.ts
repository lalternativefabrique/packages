import { afterEach, describe, expect, it } from "vitest";
import { clearGuide, readScreenFields, showGuide } from "./guide";
import { applyEvent } from "./stream";

const boxed = (el: HTMLElement) => {
  el.getBoundingClientRect = () => ({ x: 10, y: 10, left: 10, top: 10, width: 200, height: 30, right: 210, bottom: 40, toJSON: () => ({}) });
  el.scrollIntoView = () => {};
};

function form() {
  document.body.innerHTML = `<main>
    <label for="n">Nom du projet</label><input id="n" name="name" required>
    <label for="p">Mot de passe</label><input id="p" type="password" value="secret">
    <select aria-label="Catégorie"><option>Art</option><option>Tech</option></select>
    <input type="hidden" name="csrf" value="x">
    <div class="lalt-assistant"><textarea aria-label="Message"></textarea></div>
  </main>`;
  for (const el of document.querySelectorAll<HTMLElement>("input, select, textarea")) boxed(el);
  return document.querySelector("main") as HTMLElement;
}

afterEach(() => clearGuide());

describe("readScreenFields", () => {
  it("lists the visible fields with a ref, never a secret's value", () => {
    const fields = readScreenFields(form());
    expect(fields.map((f) => f.label)).toEqual(["Nom du projet", "Mot de passe", "Catégorie"]);
    expect(fields[0]).toMatchObject({ ref: "0", required: true, value: "" });
    expect(fields[1]).toMatchObject({ protected: true });
    expect(fields[1].value).toBeUndefined();
    expect(fields[2].options).toEqual(["Art", "Tech"]);
  });
});

describe("showGuide", () => {
  it("rings the named fields, reports the unknown ones and closes on Escape", () => {
    readScreenFields(form());
    const result = showGuide([
      { field: "0", note: "Un titre court" },
      { field: "9", note: "absent" },
    ]);
    expect(result).toMatchObject({ shown: ["0"], missing: ["9"] });
    expect(document.getElementById("lalt-assistant-guide")).not.toBeNull();
    expect(document.activeElement?.id).toBe("n");
    dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(document.getElementById("lalt-assistant-guide")).toBeNull();
  });

  it("shows nothing when no field matches", () => {
    readScreenFields(form());
    expect(showGuide([{ field: "42", note: "x" }]).shown).toEqual([]);
    expect(document.getElementById("lalt-assistant-guide")).toBeNull();
  });

  it("is kept on the message so it can be shown again", () => {
    const steps = [{ field: "0", note: "Un titre" }];
    expect(applyEvent({ role: "assistant", content: "" }, { type: "guide", steps }, 0).guide).toEqual(steps);
  });
});
