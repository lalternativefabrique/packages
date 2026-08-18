import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { placeToolbar, type SelectionRect } from "./placement";
import { useViewport } from "./useViewport";

export interface ToolbarAction {
  id: string;
  label: string;
  icon?: React.ReactNode;
  onSelect: (selectedText: string) => void;
}

export interface BlockFormat {
  id: string;
  label: string;
  shortcut?: string;
  isActive?: boolean;
  onSelect: () => void;
}

export interface SelectionToolbarProps {
  selectedText: string;
  selection: SelectionRect;
  actions?: ToolbarAction[];
  /** Heading levels and list kinds, rendered behind a single trigger. */
  formats?: BlockFormat[];
  formatsLabel?: string;
  /** iOS draws its own callout above a selection; the host detects it. */
  preferBelow?: boolean;
  container?: HTMLElement | null;
}

const FALLBACK = { width: 320, height: 40 };

// Whether the width used to place the bar is the one it actually has. The
// fallback is a guess, and placing on a guess puts the bar off-centre by
// however far the guess is wrong — which is invisible while the bar happens to
// be 320px wide and obvious as soon as a host adds another action to it.
type Measured = { width: number; height: number; real: boolean };

export function SelectionToolbar({
  selectedText,
  selection,
  actions = [],
  formats = [],
  formatsLabel = "Format",
  preferBelow = false,
  container,
}: SelectionToolbarProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<Measured>({ ...FALLBACK, real: false });
  const [formatsOpen, setFormatsOpen] = useState(false);
  const viewport = useViewport();

  // Measured after paint rather than assumed: the toolbar's width depends on
  // labels the host supplies, and guessing it puts the toolbar off-centre.
  //
  // Observed rather than recomputed from a dependency list: the width follows
  // the rendered labels, and a list naming the things that happen to change it
  // today misses the next one — a longer label, a font that loads late.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      if (rect.width > 0)
        setSize((prev) =>
          prev.real && prev.width === rect.width && prev.height === rect.height
            ? prev
            : { width: rect.width, height: rect.height, real: true },
        );
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!formatsOpen) return;
    const close = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setFormatsOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [formatsOpen]);

  if (typeof document === "undefined") return null;

  const place = placeToolbar({
    selection,
    toolbar: size,
    viewport: { width: viewport.width, height: viewport.height },
    preferBelow,
  });

  return createPortal(
    <div
      ref={ref}
      role="toolbar"
      className="lalt-toolbar"
      // Custom properties rather than left/top directly: under the mobile
      // breakpoint the stylesheet docks the toolbar to the bottom of the
      // screen and ignores these, which inline left/top would override.
      //
      // The inset is what the docked bar sits on top of. `bottom: 0` means the
      // bottom of the layout viewport, which the on-screen keyboard covers
      // without shrinking — so the bar has to be lifted by the strip the
      // keyboard occupies to stay in sight.
      style={
        {
          "--lalt-toolbar-left": `${place.left}px`,
          "--lalt-toolbar-top": `${place.top}px`,
          "--lalt-keyboard-inset": `${viewport.bottomInset}px`,
          // Hidden for the one frame between mounting and being measured:
          // that first paint places the bar on the fallback width, and showing
          // it means showing the bar jump once the real width arrives.
          // Hidden rather than unmounted — it has to be in the document to
          // have a width at all.
          visibility: size.real ? undefined : "hidden",
        } as React.CSSProperties
      }
    >
      {actions.map((action) => (
        <button
          key={action.id}
          type="button"
          className="lalt-toolbar__button"
          // The label is what the narrow layout hides, so it has to stay
          // reachable to anyone not reading the icon.
          aria-label={action.icon ? action.label : undefined}
          data-has-icon={action.icon ? "" : undefined}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => action.onSelect(selectedText)}
        >
          {action.icon}
          <span className="lalt-toolbar__label">{action.label}</span>
        </button>
      ))}

      {formats.length > 0 && (
        <div className="lalt-toolbar__formats">
          <button
            type="button"
            className="lalt-toolbar__button"
            aria-haspopup="menu"
            aria-expanded={formatsOpen}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => setFormatsOpen((open) => !open)}
          >
            {formatsLabel}
          </button>
          {formatsOpen && (
            <div role="menu" className="lalt-toolbar__menu">
              {formats.map((format) => (
                <button
                  key={format.id}
                  type="button"
                  role="menuitem"
                  className="lalt-toolbar__item"
                  aria-current={format.isActive}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => {
                    format.onSelect();
                    setFormatsOpen(false);
                  }}
                >
                  <span>{format.label}</span>
                  {format.shortcut && (
                    <kbd className="lalt-toolbar__shortcut">{format.shortcut}</kbd>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>,
    container ?? document.body,
  );
}
