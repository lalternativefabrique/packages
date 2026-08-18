export interface SelectionRect {
  /** Horizontal centre of the selection. */
  x: number;
  top: number;
  bottom: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface PlaceToolbarInput {
  selection: SelectionRect;
  toolbar: Size;
  viewport: Size;
  /** iOS renders its own callout above a selection, which covers a toolbar
   *  placed there, so the caller asks for below on that platform. */
  preferBelow?: boolean;
  /** Height of the system's own selection callout — Cut/Copy/Paste — which is
   *  drawn over the page and cannot be measured from it. Clearing it is what
   *  keeps the two from overlapping. Zero where there is no such callout. */
  calloutHeight?: number;
}

export interface ToolbarPlacement {
  left: number;
  top: number;
  below: boolean;
}

const GAP = 8;

/** iOS draws its Cut/Copy/Paste callout roughly this tall, below a selection
 *  the page cannot see. Measured from the platform rather than derived: it is
 *  a system surface, so nothing in the document reports its size. */
export const IOS_CALLOUT_HEIGHT = 44;

/**
 * placeToolbar positions a floating toolbar against a selection.
 *
 * Kept apart from the component because it is the part that is wrong in subtle
 * ways — off-screen at the edges, hidden under a system callout — and the part
 * a test can pin down without a browser.
 */
export function placeToolbar({
  selection,
  toolbar,
  viewport,
  preferBelow = false,
  calloutHeight = 0,
}: PlaceToolbarInput): ToolbarPlacement {
  const clearance = GAP + calloutHeight;
  const fitsAbove = selection.top - GAP - toolbar.height >= 0;
  const fitsBelow = selection.bottom + clearance + toolbar.height <= viewport.height;

  const below = preferBelow ? fitsBelow || !fitsAbove : !fitsAbove && fitsBelow;

  const top = below
    ? Math.min(
        selection.bottom + clearance,
        // Never past the bottom edge: a selection low on the page would put
        // the toolbar off-screen once the callout's height is added.
        Math.max(0, viewport.height - toolbar.height - GAP),
      )
    : Math.max(0, selection.top - GAP - toolbar.height);

  const centred = selection.x - toolbar.width / 2;
  const left = Math.max(0, Math.min(centred, viewport.width - toolbar.width));

  return { left, top, below };
}
