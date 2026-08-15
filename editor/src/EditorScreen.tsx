import { useViewport } from "./useViewport";

export type SaveState = "idle" | "saving" | "saved" | "dirty";

export interface EditorScreenProps {
  /** The writing surface. Mounted by the host, which owns the TipTap instance
   *  and whichever extensions it needs. */
  children: React.ReactNode;
  title?: string;
  onTitleChange?: (title: string) => void;
  titlePlaceholder?: string;
  titleLabel?: string;
  saveState?: SaveState;
  saveLabels?: SaveLabels;
  /** Drawn at the far left of the bar; a back control belongs here. */
  lead?: React.ReactNode;
  /** Drawn at the far right. Keep it to what a thumb can reach on a phone —
   *  one primary control and an overflow, not a row of five. */
  actions?: React.ReactNode;
  /** Sits between the save dot and the actions: a character count, a badge. */
  status?: React.ReactNode;
  /** Between the title and the writing surface — a dictation control, a hint. */
  aside?: React.ReactNode;
  /** Below the writing surface, inside the scrolling column. */
  footer?: React.ReactNode;
  /** Above the writing surface and below the bar, pinned while the page
   *  scrolls: tabs over variants of one document belong here. */
  subnav?: React.ReactNode;
  /** Fills its container instead of the viewport, for a host whose shell
   *  already owns the page height. */
  fill?: boolean;
  className?: string;
}

export interface SaveLabels {
  saving: string;
  saved: string;
  dirty: string;
}

export const defaultSaveLabels: SaveLabels = {
  saving: "Enregistrement en cours",
  saved: "Enregistré",
  dirty: "Modifications non enregistrées",
};

/**
 * EditorScreen is the page a writing surface sits in: a command bar, a title,
 * one column of prose, and slots for whatever the host puts around it.
 *
 * It renders and measures; it never fetches, saves or routes. `saveState` is
 * displayed, not computed — the host owns the debounce and the request, so the
 * same screen serves an autosaving draft and a form with an explicit button.
 *
 * Commands sit at the top rather than in a bar at the bottom. Below the fold
 * they are gone the moment the document runs past one screen, and pinned to
 * the bottom they eat a sixth of what the on-screen keyboard leaves.
 */
export function EditorScreen({
  children,
  title,
  onTitleChange,
  titlePlaceholder = "Titre",
  titleLabel = "Titre",
  saveState,
  saveLabels = defaultSaveLabels,
  lead,
  actions,
  status,
  aside,
  footer,
  subnav,
  fill = false,
  className,
}: EditorScreenProps) {
  const { bottomInset } = useViewport();
  const withTitle = onTitleChange !== undefined;

  return (
    <div
      className={[
        "lalt-screen",
        fill ? "lalt-screen--fill" : "",
        className ?? "",
      ]
        .filter(Boolean)
        .join(" ")}
      // The keyboard covers the layout viewport without shrinking it, so the
      // column's last lines sit under the keys unless the strip is padded out.
      style={{ "--lalt-keyboard": `${bottomInset}px` } as React.CSSProperties}
    >
      <header className="lalt-screen__bar">
        {lead}
        {saveState && (
          <SaveDot state={saveState} labels={saveLabels} />
        )}
        {status && <div className="lalt-screen__status">{status}</div>}
        {actions && <div className="lalt-screen__actions">{actions}</div>}
      </header>

      {subnav && <div className="lalt-screen__subnav">{subnav}</div>}

      <div className="lalt-screen__scroll">
        <div className="lalt-screen__column">
          {withTitle && (
            <input
              className="lalt-screen__title"
              value={title ?? ""}
              onChange={(event) => onTitleChange(event.target.value)}
              placeholder={titlePlaceholder}
              aria-label={titleLabel}
            />
          )}
          {aside && <div className="lalt-screen__aside">{aside}</div>}
          {children}
          {footer && <div className="lalt-screen__footer">{footer}</div>}
        </div>
      </div>
    </div>
  );
}

/**
 * A dot rather than a word: the state is worth a glance, never a read, and a
 * label that appears and vanishes leaves the author wondering what it meant
 * once it is gone. Screen readers get the sentence instead.
 */
function SaveDot({ state, labels }: { state: SaveState; labels: SaveLabels }) {
  if (state === "idle") return <span className="lalt-screen__save" />;

  const label =
    state === "saving" ? labels.saving : state === "dirty" ? labels.dirty : labels.saved;

  return (
    <span className="lalt-screen__save" role="status" aria-live="polite">
      <span className={`lalt-screen__dot lalt-screen__dot--${state}`} aria-hidden />
      <span className="lalt-screen__sr">{label}</span>
    </span>
  );
}
