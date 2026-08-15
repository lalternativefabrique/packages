export interface VisualViewportLike {
  width: number;
  height: number;
  offsetTop: number;
}

export interface ViewportMetrics {
  width: number;
  height: number;
  offsetTop: number;
  /** How much of the layout viewport's bottom is covered, the on-screen
   *  keyboard included. Zero on a desktop browser and wherever the API is
   *  missing. */
  bottomInset: number;
}

export interface ReadViewportInput {
  innerWidth: number;
  innerHeight: number;
  visual?: VisualViewportLike | null;
}

/**
 * readViewport reports the area a fixed element can actually occupy.
 *
 * The distinction the on-screen keyboard turns on: `innerHeight` measures the
 * layout viewport, which the keyboard covers without shrinking, so an element
 * pinned to its bottom edge ends up underneath the keys. Only `visualViewport`
 * narrows, and the difference is the inset a docked toolbar has to clear.
 */
export function readViewport({
  innerWidth,
  innerHeight,
  visual,
}: ReadViewportInput): ViewportMetrics {
  if (!visual) {
    return { width: innerWidth, height: innerHeight, offsetTop: 0, bottomInset: 0 };
  }

  // offsetTop is what iOS scrolls the visual viewport by; without it the
  // covered strip reads as smaller than it is once the page is scrolled under
  // a raised keyboard. Never negative: a viewport reported as taller than the
  // layout one — Android during a rotation, a browser mid-resize — would
  // otherwise push the toolbar down off the screen.
  const covered = innerHeight - visual.height - visual.offsetTop;

  return {
    width: visual.width,
    height: visual.height,
    offsetTop: visual.offsetTop,
    bottomInset: Math.max(0, Math.round(covered)),
  };
}
