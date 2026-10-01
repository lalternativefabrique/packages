import { KeyboardAvoidingView, Modal, Platform, Pressable, StyleSheet, Text, View } from "react-native";

import { formatActions, type FormatAction } from "./format";
import { defaultEditorToolbarTheme, type EditorToolbarTheme } from "./theme";

export interface FormatSheetProps {
  visible: boolean;
  onClose: () => void;
  onPick: (action: FormatAction) => void;
  actions?: FormatAction[];
  theme?: EditorToolbarTheme;
}

export function FormatSheet({
  visible,
  onClose,
  onPick,
  actions = formatActions(),
  theme = defaultEditorToolbarTheme,
}: FormatSheetProps) {
  return (
    <Modal visible={visible} animationType="slide" transparent onRequestClose={onClose}>
      <Pressable style={[styles.backdrop, { backgroundColor: theme.backdrop }]} onPress={onClose} />
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : undefined}
        style={styles.sheetWrapper}
        pointerEvents="box-none"
      >
        <View style={[styles.sheet, { backgroundColor: theme.surface }]}>
          <View style={[styles.handle, { backgroundColor: theme.border }]} />
          {actions.map((action) => (
            <Pressable
              key={action.id}
              accessibilityRole="button"
              onPress={() => onPick(action)}
              style={({ pressed }) => [styles.row, pressed && { backgroundColor: theme.pressed }]}
            >
              <Text style={[styles.glyph, { color: theme.muted }]}>{action.glyph}</Text>
              <Text style={[styles.label, { color: theme.text }]}>{action.label}</Text>
              <Text style={[styles.shortcut, { color: theme.muted, backgroundColor: theme.pressed }]}>
                {action.shortcut}
              </Text>
            </Pressable>
          ))}
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { ...StyleSheet.absoluteFillObject },
  sheetWrapper: { flex: 1, justifyContent: "flex-end" },
  sheet: { borderTopLeftRadius: 20, borderTopRightRadius: 20, paddingHorizontal: 8, paddingBottom: 40 },
  handle: { alignSelf: "center", width: 40, height: 4, borderRadius: 999, marginTop: 8, marginBottom: 12 },
  row: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 12, paddingVertical: 12, borderRadius: 10 },
  glyph: { width: 24, fontSize: 14, fontWeight: "600", textAlign: "center" },
  label: { flex: 1, fontSize: 15 },
  shortcut: { fontSize: 12, fontFamily: Platform.OS === "ios" ? "Menlo" : "monospace", paddingHorizontal: 6, paddingVertical: 2, borderRadius: 4, overflow: "hidden" },
});
