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
}

export interface ToolbarPlacement {
  left: number;
  top: number;
  below: boolean;
}

const GAP = 8;

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
}: PlaceToolbarInput): ToolbarPlacement {
  const fitsAbove = selection.top - GAP - toolbar.height >= 0;
  const fitsBelow = selection.bottom + GAP + toolbar.height <= viewport.height;

  const below = preferBelow ? fitsBelow || !fitsAbove : !fitsAbove && fitsBelow;

  const top = below
    ? selection.bottom + GAP
    : Math.max(0, selection.top - GAP - toolbar.height);

  const centred = selection.x - toolbar.width / 2;
  const left = Math.max(0, Math.min(centred, viewport.width - toolbar.width));

  return { left, top, below };
}
