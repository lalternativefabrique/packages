import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from "react";

import type { SlashItem } from "./slash";

export interface SlashMenuHandle {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export interface SlashMenuProps {
  items: SlashItem[];
  command: (item: SlashItem) => void;
  emptyLabel?: string;
}

export const SlashMenu = forwardRef<SlashMenuHandle, SlashMenuProps>(
  function SlashMenu({ items, command, emptyLabel = "Aucune commande" }, ref) {
    const [selectedIndex, setSelectedIndex] = useState(0);

    // The suggestion plugin rebuilds these props on every keystroke, so the
    // imperative handle reads refs rather than a captured render closure —
    // otherwise Enter runs the command the menu showed one keystroke ago.
    const itemsRef = useRef(items);
    itemsRef.current = items;
    const commandRef = useRef(command);
    commandRef.current = command;
    const selectedRef = useRef(selectedIndex);
    selectedRef.current = selectedIndex;

    const ids = items.map((item) => item.id).join(" ");
    useEffect(() => {
      setSelectedIndex(0);
      selectedRef.current = 0;
    }, [ids]);

    const selectItem = useCallback((index: number) => {
      const item = itemsRef.current[index];
      if (item) commandRef.current(item);
    }, []);

    const move = useCallback((delta: number) => {
      const count = itemsRef.current.length;
      if (count === 0) return;
      const next = (selectedRef.current + delta + count) % count;
      selectedRef.current = next;
      setSelectedIndex(next);
    }, []);

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event }) => {
        if (event.key === "ArrowUp") {
          move(-1);
          return true;
        }
        if (event.key === "ArrowDown") {
          move(1);
          return true;
        }
        if (event.key === "Enter" || event.key === "Tab") {
          if (itemsRef.current.length === 0) return false;
          selectItem(selectedRef.current);
          return true;
        }
        return false;
      },
    }));

    if (items.length === 0) {
      return <div className="lalt-slash lalt-slash--empty">{emptyLabel}</div>;
    }

    return (
      <div className="lalt-slash" role="listbox">
        {items.map((item, index) => (
          <button
            key={item.id}
            type="button"
            role="option"
            aria-selected={index === selectedIndex}
            className={
              index === selectedIndex
                ? "lalt-slash__item lalt-slash__item--active"
                : "lalt-slash__item"
            }
            // Losing editor focus tears the suggestion plugin down before a
            // click lands, so the item would never run its command.
            onMouseDown={(event) => {
              event.preventDefault();
              selectItem(index);
            }}
            onMouseEnter={() => {
              selectedRef.current = index;
              setSelectedIndex(index);
            }}
          >
            {item.icon && (
              <span className="lalt-slash__icon" aria-hidden>
                {item.icon}
              </span>
            )}
            <span className="lalt-slash__text">
              <span className="lalt-slash__title">{item.title}</span>
              {item.description && (
                <span className="lalt-slash__description">{item.description}</span>
              )}
            </span>
            {item.shortcut && (
              <kbd className="lalt-slash__shortcut">{item.shortcut}</kbd>
            )}
          </button>
        ))}
      </div>
    );
  },
);
