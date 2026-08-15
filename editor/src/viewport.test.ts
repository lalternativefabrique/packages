import { describe, expect, it } from "vitest";

import { readViewport } from "./viewport";

describe("readViewport", () => {
  it("falls back to the layout viewport where the API is missing", () => {
    const metrics = readViewport({ innerWidth: 1000, innerHeight: 800, visual: null });

    expect(metrics).toEqual({ width: 1000, height: 800, offsetTop: 0, bottomInset: 0 });
  });

  it("reports no inset when nothing covers the viewport", () => {
    const metrics = readViewport({
      innerWidth: 390,
      innerHeight: 844,
      visual: { width: 390, height: 844, offsetTop: 0 },
    });

    expect(metrics.bottomInset).toBe(0);
  });

  it("reports the keyboard as the strip it covers", () => {
    const metrics = readViewport({
      innerWidth: 390,
      innerHeight: 844,
      visual: { width: 390, height: 544, offsetTop: 0 },
    });

    expect(metrics.bottomInset).toBe(300);
    expect(metrics.height).toBe(544);
  });

  it("counts the offset the page is scrolled under a raised keyboard", () => {
    const metrics = readViewport({
      innerWidth: 390,
      innerHeight: 844,
      visual: { width: 390, height: 544, offsetTop: 120 },
    });

    expect(metrics.bottomInset).toBe(180);
  });

  it("never reports a negative inset when the visual viewport reads taller", () => {
    const metrics = readViewport({
      innerWidth: 390,
      innerHeight: 500,
      visual: { width: 390, height: 844, offsetTop: 0 },
    });

    expect(metrics.bottomInset).toBe(0);
  });

  it("rounds the inset, which arrives fractional on a scaled viewport", () => {
    const metrics = readViewport({
      innerWidth: 390,
      innerHeight: 844,
      visual: { width: 390, height: 543.6, offsetTop: 0 },
    });

    expect(metrics.bottomInset).toBe(300);
  });
});
