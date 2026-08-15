import { Extension } from "@tiptap/core";
import { ReactRenderer } from "@tiptap/react";
import { PluginKey } from "@tiptap/pm/state";
import Suggestion, { type SuggestionOptions, type SuggestionProps } from "@tiptap/suggestion";

import { SlashMenu, type SlashMenuHandle, type SlashMenuProps } from "./SlashMenu";
import { filterSlashItems, type SlashItem } from "./slash";

export interface SlashCommandsOptions {
  /** Read on every keystroke, so a host can swap the set without remounting
   *  TipTap — rebuilding the extensions array wipes the text being typed. */
  items: () => SlashItem[];
  char: string;
  emptyLabel?: string;
  /** Distinguishes concurrently mounted editors: two surfaces sharing one key
   *  would answer each other's suggestions. */
  pluginKey: PluginKey;
}

const OFFSET = 6;

export const SlashCommands = Extension.create<SlashCommandsOptions>({
  name: "laSlashCommands",

  addOptions() {
    return {
      items: () => [],
      char: "/",
      pluginKey: new PluginKey("laSlashCommands"),
    };
  },

  addProseMirrorPlugins() {
    const { items, char, emptyLabel, pluginKey } = this.options;

    const suggestion: Omit<SuggestionOptions<SlashItem>, "editor"> = {
      char,
      pluginKey,
      startOfLine: false,
      items: ({ query }) => filterSlashItems(items(), query),
      render: () => {
        let component: ReactRenderer<SlashMenuHandle, SlashMenuProps> | null = null;
        let container: HTMLDivElement | null = null;

        const place = (clientRect: SuggestionProps<SlashItem>["clientRect"]) => {
          if (!container || !clientRect) return;
          const rect = clientRect();
          if (!rect) return;
          container.style.top = `${rect.bottom + OFFSET}px`;
          container.style.left = `${rect.left}px`;
        };

        const close = () => {
          container?.remove();
          container = null;
          component?.destroy();
          component = null;
        };

        return {
          onStart: (props) => {
            component = new ReactRenderer(SlashMenu, {
              props: { ...props, emptyLabel },
              editor: props.editor,
            });
            container = document.createElement("div");
            container.className = "lalt-slash-anchor";
            container.appendChild(component.element);
            document.body.appendChild(container);
            place(props.clientRect);
          },
          onUpdate: (props) => {
            component?.updateProps({ ...props, emptyLabel });
            place(props.clientRect);
          },
          onKeyDown: (props) => {
            if (props.event.key === "Escape") {
              close();
              return true;
            }
            return component?.ref?.onKeyDown(props) ?? false;
          },
          onExit: close,
        };
      },
    };

    return [
      Suggestion({
        editor: this.editor,
        ...suggestion,
        command: ({ editor, range, props }) => props.command({ editor, range }),
      }),
    ];
  },
});
