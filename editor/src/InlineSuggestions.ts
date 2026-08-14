import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";

export interface InlineSuggestion {
  id: string;
  /** Document positions of the passage this replaces. */
  from: number;
  to: number;
  after: string;
}

export interface SuggestionActions {
  onAccept: (id: string) => void;
  onReject: (id: string) => void;
  acceptLabel: string;
  rejectLabel: string;
}

export const inlineSuggestionsKey = new PluginKey<DecorationSet>("lalt-inline-suggestions");

/**
 * InlineSuggestions renders proposed edits where they belong: over the passage
 * they replace, with the new text right after it.
 *
 * Decorations, never document changes. The editor's content stays exactly what
 * the author wrote until they accept, so the draft is saveable mid-review and
 * a rejected suggestion leaves nothing behind — which a document-level insert
 * could not promise.
 */
export const InlineSuggestions = Extension.create<{ actions: SuggestionActions | null }>({
  name: "lalt-inline-suggestions",

  addOptions() {
    return { actions: null };
  },

  addProseMirrorPlugins() {
    const options = this.options;

    return [
      new Plugin<DecorationSet>({
        key: inlineSuggestionsKey,

        state: {
          init: () => DecorationSet.empty,
          apply(tr, old) {
            const set = tr.getMeta(inlineSuggestionsKey) as InlineSuggestion[] | undefined;
            if (set) return build(tr.doc, set, options.actions);
            // Positions follow the document as the author keeps typing around
            // a pending suggestion.
            return old.map(tr.mapping, tr.doc);
          },
        },

        props: {
          decorations(state) {
            return inlineSuggestionsKey.getState(state) ?? DecorationSet.empty;
          },
        },
      }),
    ];
  },
});

function build(
  doc: ProseMirrorNode,
  suggestions: InlineSuggestion[],
  actions: SuggestionActions | null,
): DecorationSet {
  const decorations: Decoration[] = [];

  for (const s of suggestions) {
    if (s.from < 0 || s.to > doc.content.size || s.from >= s.to) continue;

    decorations.push(
      Decoration.inline(s.from, s.to, { class: "lalt-suggestion__old" }),
      Decoration.widget(s.to, () => renderProposal(s, actions), {
        // The widget belongs to the suggestion that precedes it, so typing at
        // the boundary does not push it around.
        side: 1,
        ignoreSelection: true,
      }),
    );
  }
  return DecorationSet.create(doc, decorations);
}

function renderProposal(s: InlineSuggestion, actions: SuggestionActions | null): HTMLElement {
  const wrap = document.createElement("span");
  wrap.className = "lalt-suggestion";
  wrap.setAttribute("contenteditable", "false");

  const text = document.createElement("span");
  text.className = "lalt-suggestion__new";
  text.textContent = s.after;
  wrap.appendChild(text);

  if (actions) {
    wrap.appendChild(button("reject", actions.rejectLabel, "✕", () => actions.onReject(s.id)));
    wrap.appendChild(button("accept", actions.acceptLabel, "✓", () => actions.onAccept(s.id)));
  }
  return wrap;
}

function button(kind: string, label: string, glyph: string, onClick: () => void): HTMLElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `lalt-suggestion__action lalt-suggestion__action--${kind}`;
  el.setAttribute("aria-label", label);
  el.textContent = glyph;
  // mousedown would move the selection into the widget before the click lands.
  el.addEventListener("mousedown", (e) => e.preventDefault());
  el.addEventListener("click", (e) => {
    e.preventDefault();
    onClick();
  });
  return el;
}
