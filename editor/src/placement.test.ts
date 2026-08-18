import { describe, expect, it } from "vitest";

import { placeToolbar } from "./placement";

const viewport = { width: 1000, height: 800 };
const toolbar = { width: 200, height: 40 };
const selection = { x: 500, top: 400, bottom: 420 };

describe("placeToolbar", () => {
  it("centres the toolbar over the selection", () => {
    const place = placeToolbar({ selection, toolbar, viewport });

    expect(place.left + toolbar.width / 2).toBe(selection.x);
  });

  it("sits above the selection when there is room", () => {
    const place = placeToolbar({ selection, toolbar, viewport });

    expect(place.top + toolbar.height).toBeLessThanOrEqual(selection.top);
    expect(place.below).toBe(false);
  });

  it("flips below when the selection is too close to the top", () => {
    const place = placeToolbar({
      selection: { x: 500, top: 10, bottom: 30 },
      toolbar,
      viewport,
    });

    expect(place.below).toBe(true);
    expect(place.top).toBeGreaterThanOrEqual(30);
  });

  it("keeps the toolbar inside the left edge", () => {
    const place = placeToolbar({ selection: { x: 5, top: 400, bottom: 420 }, toolbar, viewport });

    expect(place.left).toBeGreaterThanOrEqual(0);
  });

  it("keeps the toolbar inside the right edge", () => {
    const place = placeToolbar({ selection: { x: 995, top: 400, bottom: 420 }, toolbar, viewport });

    expect(place.left + toolbar.width).toBeLessThanOrEqual(viewport.width);
  });

  it("prefers below on iOS, where the system callout covers what is above", () => {
    const place = placeToolbar({ selection, toolbar, viewport, preferBelow: true });

    expect(place.below).toBe(true);
    expect(place.top).toBeGreaterThanOrEqual(selection.bottom);
  });

  it("flips back above when below would overflow the bottom", () => {
    const place = placeToolbar({
      selection: { x: 500, top: 760, bottom: 790 },
      toolbar,
      viewport,
      preferBelow: true,
    });

    expect(place.below).toBe(false);
    expect(place.top + toolbar.height).toBeLessThanOrEqual(viewport.height);
  });

  it("never leaves the toolbar off-screen when the viewport is narrower than it", () => {
    const place = placeToolbar({ selection, toolbar: { width: 1200, height: 40 }, viewport });

    expect(place.left).toBe(0);
  });

  // The bar is centred on the selection, so its left edge is computed from its
  // width. Placing it before it has been measured is what put it off-centre by
  // half the difference the moment a host added another action to it.
  it("centres on the selection using the width it was given", () => {
    const guessed = placeToolbar({ selection, toolbar: { width: 320, height: 40 }, viewport });
    const actual = placeToolbar({ selection, toolbar: { width: 460, height: 40 }, viewport });

    expect(actual.left).toBe(guessed.left - 70);
  });

  // Below is not enough on iOS: the system draws Cut/Copy/Paste in that space,
  // over the page and unmeasurable from it, so a toolbar placed against the
  // selection lands underneath the callout rather than below it.
  it("clears the system callout when placing below", () => {
    const tight = placeToolbar({ selection, toolbar, viewport, preferBelow: true });
    const cleared = placeToolbar({
      selection,
      toolbar,
      viewport,
      preferBelow: true,
      calloutHeight: 44,
    });

    expect(cleared.top).toBe(tight.top + 44);
    expect(cleared.top).toBeGreaterThanOrEqual(selection.bottom + 44);
  });

  it("keeps the toolbar on screen when the callout would push it off", () => {
    const place = placeToolbar({
      selection: { x: 500, top: 700, bottom: 740 },
      toolbar,
      viewport,
      preferBelow: true,
      calloutHeight: 44,
    });

    expect(place.top + toolbar.height).toBeLessThanOrEqual(viewport.height);
  });
});
