import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { placeToolbar, type SelectionRect } from "./placement";

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
  const [size, setSize] = useState(FALLBACK);
  const [formatsOpen, setFormatsOpen] = useState(false);

  // Measured after paint rather than assumed: the toolbar's width depends on
  // labels the host supplies, and guessing it puts the toolbar off-centre.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    if (rect.width > 0) setSize({ width: rect.width, height: rect.height });
  }, [selectedText, actions.length, formats.length]);

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
    viewport: { width: window.innerWidth, height: window.innerHeight },
    preferBelow,
  });

  return createPortal(
    <div
      ref={ref}
      role="toolbar"
      className="lalt-toolbar"
      style={{ position: "fixed", left: place.left, top: place.top, zIndex: 100 }}
    >
      {actions.map((action) => (
        <button
          key={action.id}
          type="button"
          className="lalt-toolbar__button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => action.onSelect(selectedText)}
        >
          {action.icon}
          <span>{action.label}</span>
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
