import { useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View, type TextStyle } from "react-native";

import { formatActions, type FormatAction } from "./format";
import { defaultEditorToolbarTheme, type EditorToolbarTheme } from "./theme";

export interface EditorToolbarControls {
  close: () => void;
}

export interface EditorToolbarProps {
  onFormat: (action: FormatAction) => void;
  actions?: FormatAction[];
  /** Buttons the host adds after the format toggle: dictation, assistant, save. */
  children?: React.ReactNode;
  /** A host panel shown in place of the bar while `panelOpen` is true. */
  renderPanel?: (controls: EditorToolbarControls) => React.ReactNode;
  panelOpen?: boolean;
  onPanelClose?: () => void;
  bottom?: number;
  theme?: EditorToolbarTheme;
  formatLabel?: string;
  backLabel?: string;
  onPressFeedback?: () => void;
}

function glyphStyle(action: FormatAction): TextStyle {
  switch (action.id) {
    case "bold":
      return { fontSize: 17, fontWeight: "700" };
    case "italic":
      return { fontSize: 17, fontStyle: "italic", fontWeight: "500" };
    case "underline":
      return { fontSize: 17, fontWeight: "500", textDecorationLine: "underline" };
    case "strike":
      return { fontSize: 17, fontWeight: "500", textDecorationLine: "line-through" };
    case "h1":
    case "h2":
    case "h3":
    case "h4":
      return { fontSize: 14, fontWeight: "600" };
    default:
      return { fontSize: 15, fontWeight: "500" };
  }
}

export function EditorToolbar({
  onFormat,
  actions = formatActions(),
  children,
  renderPanel,
  panelOpen = false,
  onPanelClose,
  bottom = 0,
  theme = defaultEditorToolbarTheme,
  formatLabel = "Aa",
  backLabel = "‹",
  onPressFeedback,
}: EditorToolbarProps) {
  const [formatOpen, setFormatOpen] = useState(false);
  const pill = [styles.pill, { backgroundColor: theme.surface, borderColor: theme.border }];
  const pressed = ({ pressed }: { pressed: boolean }) => [styles.icon, pressed && styles.pressed];

  const back = (onPress: () => void) => (
    <>
      <Pressable accessibilityRole="button" onPress={onPress} style={pressed}>
        <Text style={[styles.back, { color: theme.muted }]}>{backLabel}</Text>
      </Pressable>
      <View style={[styles.divider, { backgroundColor: theme.border }]} />
    </>
  );

  if (panelOpen && renderPanel) {
    return (
      <View style={[styles.wrap, { bottom }]}>
        {renderPanel({ close: () => onPanelClose?.() })}
      </View>
    );
  }

  return (
    <View style={[styles.wrap, { bottom }]}>
      <View style={styles.pillRow}>
        <View style={pill}>
          {formatOpen ? (
            <>
              {back(() => setFormatOpen(false))}
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                keyboardShouldPersistTaps="always"
                contentContainerStyle={styles.formatRow}
              >
                {actions.map((action) => (
                  <Pressable
                    key={action.id}
                    accessibilityRole="button"
                    accessibilityLabel={action.label}
                    onPress={() => {
                      onPressFeedback?.();
                      onFormat(action);
                    }}
                    style={pressed}
                  >
                    <Text style={[glyphStyle(action), { color: theme.text }]}>{action.glyph}</Text>
                  </Pressable>
                ))}
              </ScrollView>
            </>
          ) : (
            <>
              <Pressable
                accessibilityRole="button"
                onPress={() => {
                  onPressFeedback?.();
                  setFormatOpen(true);
                }}
                style={pressed}
              >
                <Text style={[styles.aa, { color: theme.text }]}>{formatLabel}</Text>
              </Pressable>
              {children}
            </>
          )}
        </View>
      </View>
    </View>
  );
}

export function EditorToolbarButton({
  onPress,
  label,
  disabled,
  children,
}: {
  onPress: () => void;
  label: string;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [styles.icon, (pressed || disabled) && styles.pressed]}
    >
      {children}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  // Android stacks by elevation, not zIndex: without it the bar renders under
  // the ScrollView it overlays.
  wrap: { position: "absolute", left: 0, right: 0, zIndex: 10, elevation: 10 },
  pillRow: { alignItems: "center", paddingHorizontal: 16 },
  pill: {
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 999,
    borderWidth: StyleSheet.hairlineWidth,
    shadowColor: "#000",
    shadowOpacity: 0.12,
    shadowRadius: 8,
    shadowOffset: { width: 0, height: 3 },
  },
  formatRow: { alignItems: "center" },
  icon: { width: 40, height: 36, alignItems: "center", justifyContent: "center", borderRadius: 999 },
  pressed: { opacity: 0.5 },
  aa: { fontSize: 17, fontWeight: "500" },
  back: { fontSize: 22, fontWeight: "400" },
  divider: { width: StyleSheet.hairlineWidth, height: 20, marginHorizontal: 2 },
});
