import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  Image,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  useColorScheme,
  type GestureResponderEvent,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { subscribe, openFeedback, type FeedbackSource, type FeedbackType, type OpenFeedbackOptions } from './events';
import { fillMax, resolveLabels, type Labels } from './labels';
import { createRageTapDetector } from './rage';
import {
  EMAIL_RE,
  MAX_ATTACHMENTS,
  MAX_ATTACHMENT_BYTES,
  checkAttachment,
  submitFeedback,
  type AttachmentRejection,
  type FeedbackAttachment,
} from './submit';
import { DARK, LIGHT, type Palette } from './theme';

export type FeedbackPlacement = 'bottom-left' | 'bottom-right' | 'none';
export type FeedbackTheme = 'light' | 'dark' | 'auto';

export interface FeedbackProviderProps {
  endpoint: string;
  projectId: string;
  apiKey?: string;
  getHeaders?: () => Record<string, string> | Promise<Record<string, string>>;
  userEmail?: string;
  lang?: string;
  labels?: Partial<Labels>;
  theme?: FeedbackTheme;
  placement?: FeedbackPlacement;
  collapsed?: boolean;
  onCollapseChange?: (collapsed: boolean) => void;
  path?: string;
  appVersion?: string;
  context?: Record<string, string>;
  pickImages?: (remaining: number) => Promise<FeedbackAttachment[]>;
  captureScreen?: () => Promise<FeedbackAttachment | null>;
  detectRageTaps?: boolean;
  onSent?: () => void;
  children: ReactNode;
}

type Status = 'idle' | 'loading' | 'sent' | 'error';

const NUDGE_DURATION_MS = 8000;
const SENT_CLOSE_DELAY_MS = 1500;
// Leaves the sheet's slide-out animation time to finish so it is not in the capture.
const CAPTURE_HIDE_DELAY_MS = 450;

let nudgedThisSession = false;

const LabelsContext = createContext<Labels | null>(null);

const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export function FeedbackProvider({
  endpoint,
  projectId,
  apiKey,
  getHeaders,
  userEmail,
  lang,
  labels: labelOverrides,
  theme = 'auto',
  placement = 'bottom-left',
  collapsed: collapsedProp = false,
  onCollapseChange,
  path = '',
  appVersion,
  context: hostContext,
  pickImages,
  captureScreen,
  detectRageTaps = true,
  onSent,
  children,
}: FeedbackProviderProps) {
  const L = useMemo(() => resolveLabels(lang, labelOverrides), [lang, labelOverrides]);
  const scheme = useColorScheme();
  const palette = (theme === 'auto' ? scheme === 'dark' : theme === 'dark') ? DARK : LIGHT;
  const styles = useMemo(() => makeStyles(palette), [palette]);
  const insets = useSafeAreaInsets();

  const [open, setOpen] = useState(false);
  const [collapsed, setCollapsedState] = useState(collapsedProp);
  const [nudging, setNudging] = useState(false);
  const [type, setType] = useState<FeedbackType>('idea');
  const [message, setMessage] = useState('');
  const [email, setEmail] = useState(userEmail ?? '');
  const [emailTouched, setEmailTouched] = useState(false);
  const [source, setSource] = useState<FeedbackSource>('manual');
  const [openContext, setOpenContext] = useState<Record<string, string>>({});
  const [attachments, setAttachments] = useState<FeedbackAttachment[]>([]);
  const [status, setStatus] = useState<Status>('idle');
  const [errorMsg, setErrorMsg] = useState('');
  const [capturing, setCapturing] = useState(false);

  const nudgeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const userCollapsed = useRef(false);
  const stateRef = useRef({ open, nudging, collapsed, placement });
  stateRef.current = { open, nudging, collapsed, placement };

  useEffect(() => setCollapsedState(collapsedProp), [collapsedProp]);

  useEffect(() => {
    if (userEmail && !emailTouched) setEmail(userEmail);
  }, [userEmail, emailTouched]);

  useEffect(
    () => () => {
      if (nudgeTimer.current) clearTimeout(nudgeTimer.current);
      if (closeTimer.current) clearTimeout(closeTimer.current);
    },
    [],
  );

  const setCollapsed = useCallback(
    (next: boolean) => {
      userCollapsed.current = next;
      setCollapsedState(next);
      onCollapseChange?.(next);
    },
    [onCollapseChange],
  );

  const stopNudge = useCallback(() => {
    if (nudgeTimer.current) clearTimeout(nudgeTimer.current);
    nudgeTimer.current = null;
    setNudging(false);
  }, []);

  const openPanel = useCallback(
    (options: OpenFeedbackOptions = {}) => {
      stopNudge();
      if (stateRef.current.collapsed) setCollapsed(false);
      if (options.type) setType(options.type);
      if (options.message !== undefined) setMessage(options.message);
      setSource(options.source ?? 'manual');
      setOpenContext(options.context ?? {});
      setStatus((s) => (s === 'loading' ? s : 'idle'));
      setOpen(true);
    },
    [setCollapsed, stopNudge],
  );

  const nudge = useCallback(() => {
    const s = stateRef.current;
    if (s.open || s.nudging || s.placement === 'none' || userCollapsed.current || nudgedThisSession) return;
    nudgedThisSession = true;
    if (s.collapsed) setCollapsedState(false);
    setNudging(true);
    nudgeTimer.current = setTimeout(stopNudge, NUDGE_DURATION_MS);
  }, [stopNudge]);

  useEffect(
    () =>
      subscribe((event) => {
        if (event.kind === 'open') openPanel(event.options);
        else nudge();
      }),
    [openPanel, nudge],
  );

  const onRageTap = useMemo(() => createRageTapDetector(nudge), [nudge]);
  const onTouchStart = useCallback(
    (e: GestureResponderEvent) => {
      const { pageX, pageY } = e.nativeEvent;
      onRageTap({ t: Date.now(), x: pageX, y: pageY });
    },
    [onRageTap],
  );

  const showError = (text: string) => {
    setErrorMsg(text);
    setStatus('error');
  };

  const rejectionText = (r: AttachmentRejection) =>
    r === 'too_many'
      ? fillMax(L.too_many_attachments, MAX_ATTACHMENTS)
      : r === 'too_large'
        ? fillMax(L.attachment_too_large, Math.round(MAX_ATTACHMENT_BYTES / 1024 / 1024))
        : L.attachment_unsupported;

  const addAttachments = (incoming: FeedbackAttachment[]) => {
    const accepted: FeedbackAttachment[] = [];
    let rejection: AttachmentRejection | null = null;
    for (const a of incoming) {
      const r = checkAttachment(a, attachments.length + accepted.length);
      if (r) rejection ??= r;
      else accepted.push(a);
    }
    if (accepted.length) setAttachments((prev) => [...prev, ...accepted]);
    if (rejection) showError(rejectionText(rejection));
    else if (status === 'error') setStatus('idle');
  };

  const onPick = async () => {
    if (!pickImages) return;
    try {
      addAttachments(await pickImages(MAX_ATTACHMENTS - attachments.length));
    } catch (err) {
      showError(err instanceof Error ? err.message : String(err));
    }
  };

  const onCapture = async () => {
    if (!captureScreen || capturing) return;
    setCapturing(true);
    setOpen(false);
    try {
      await wait(CAPTURE_HIDE_DELAY_MS);
      const shot = await captureScreen();
      if (shot) addAttachments([shot]);
    } catch (err) {
      showError(err instanceof Error ? err.message : String(err));
    } finally {
      setCapturing(false);
      setOpen(true);
    }
  };

  const emailInvalid = emailTouched && email.trim() !== '' && !EMAIL_RE.test(email.trim());

  const onSubmit = async () => {
    if (!message.trim() || status === 'loading') return;
    if (email.trim() && !EMAIL_RE.test(email.trim())) {
      setEmailTouched(true);
      return;
    }
    setStatus('loading');
    try {
      await submitFeedback({
        endpoint,
        projectId,
        apiKey,
        headers: getHeaders ? await getHeaders() : undefined,
        type,
        message,
        source,
        context: {
          os: `${Platform.OS} ${Platform.Version}`,
          ...(appVersion ? { app: appVersion } : {}),
          ...hostContext,
          ...openContext,
        },
        path,
        userIdentifier: email,
        attachments,
      });
      setStatus('sent');
      setMessage('');
      setOpenContext({});
      setEmailTouched(false);
      setAttachments([]);
      onSent?.();
      closeTimer.current = setTimeout(() => {
        setOpen(false);
        setStatus('idle');
      }, SENT_CLOSE_DELAY_MS);
    } catch {
      showError(L.send_failed);
    }
  };

  const close = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current);
    setOpen(false);
    if (status === 'sent') setStatus('idle');
  };

  const side = placement === 'bottom-right' ? 'right' : 'left';
  const launcherBottom = insets.bottom + 16;
  const canSend = message.trim() !== '' && status !== 'loading';

  return (
    <LabelsContext.Provider value={L}>
      <View style={styles.root} onTouchStart={detectRageTaps ? onTouchStart : undefined}>
        {children}
        {placement !== 'none' && !open && !capturing && collapsed && (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={L.show}
            onPress={() => setCollapsed(false)}
            style={[styles.tab, side === 'left' ? styles.tabLeft : styles.tabRight, { bottom: launcherBottom }]}
          >
            <ChatIcon color={palette.fg} />
          </Pressable>
        )}
        {placement !== 'none' && !open && !capturing && !collapsed && (
          <View
            pointerEvents="box-none"
            style={[
              styles.launcher,
              side === 'left'
                ? { left: insets.left + 16, alignItems: 'flex-start' }
                : { right: insets.right + 16, alignItems: 'flex-end' },
              { bottom: launcherBottom },
            ]}
          >
            {nudging && (
              <Pressable accessibilityRole="button" onPress={() => openPanel({ source: 'nudge' })} style={styles.nudge}>
                <Text style={styles.nudgeText}>{L.nudge}</Text>
              </Pressable>
            )}
            <View>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={L.title}
                onPress={() => openPanel({ source: nudging ? 'nudge' : 'manual' })}
                style={({ pressed }) => [styles.fab, nudging && styles.fabNudging, pressed && styles.pressed]}
              >
                <ChatIcon color={palette.accent} />
                <Text style={styles.fabText}>{L.title}</Text>
              </Pressable>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={L.hide}
                hitSlop={10}
                onPress={() => setCollapsed(true)}
                style={[styles.fabHide, side === 'left' ? { right: -6 } : { left: -6 }]}
              >
                <Text style={styles.fabHideText}>×</Text>
              </Pressable>
            </View>
          </View>
        )}
      </View>

      <Modal
        visible={open}
        transparent
        animationType="slide"
        onRequestClose={close}
        statusBarTranslucent
        navigationBarTranslucent
      >
        <KeyboardAvoidingView behavior="padding" style={styles.modalRoot}>
          <Pressable style={styles.backdrop} accessibilityLabel={L.close} onPress={close} />
          <View
            accessibilityViewIsModal
            style={[styles.sheet, { paddingBottom: insets.bottom + 16, paddingLeft: insets.left, paddingRight: insets.right }]}
          >
            <View style={styles.head}>
              <Text style={styles.headTitle} accessibilityRole="header">
                {L.title}
              </Text>
              <Pressable accessibilityRole="button" accessibilityLabel={L.close} hitSlop={12} onPress={close}>
                <Text style={styles.closeText}>×</Text>
              </Pressable>
            </View>

            {status === 'sent' ? (
              <Text style={styles.thanks} accessibilityLiveRegion="polite">
                {L.thanks}
              </Text>
            ) : (
              <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.body}>
                {status === 'error' && (
                  <Text style={styles.error} accessibilityLiveRegion="polite">
                    {errorMsg}
                  </Text>
                )}

                <View style={styles.types}>
                  {(['idea', 'bug'] as const).map((t) => (
                    <Pressable
                      key={t}
                      accessibilityRole="button"
                      accessibilityState={{ selected: type === t }}
                      onPress={() => setType(t)}
                      style={[styles.type, type === t && styles.typeActive]}
                    >
                      <Text style={[styles.typeText, type === t && styles.typeTextActive]}>{L[t]}</Text>
                    </Pressable>
                  ))}
                </View>

                <TextInput
                  value={email}
                  onChangeText={(v) => {
                    setEmail(v);
                    setEmailTouched(true);
                  }}
                  onBlur={() => setEmailTouched(true)}
                  placeholder={L.email_placeholder}
                  placeholderTextColor={palette.muted}
                  keyboardType="email-address"
                  autoCapitalize="none"
                  autoCorrect={false}
                  autoComplete="email"
                  textContentType="emailAddress"
                  style={styles.input}
                />
                {emailInvalid && <Text style={styles.emailHint}>{L.email_invalid}</Text>}

                <TextInput
                  value={message}
                  onChangeText={setMessage}
                  placeholder={type === 'bug' ? L.placeholder_bug : L.placeholder}
                  placeholderTextColor={palette.muted}
                  multiline
                  textAlignVertical="top"
                  style={[styles.input, styles.textarea]}
                />

                {(captureScreen || pickImages) && (
                  <View style={styles.actions}>
                    {captureScreen && (
                      <Pressable
                        accessibilityRole="button"
                        accessibilityLabel={L.capture}
                        disabled={capturing || attachments.length >= MAX_ATTACHMENTS}
                        onPress={onCapture}
                        style={styles.action}
                      >
                        <Text style={styles.actionText}>{capturing ? L.capturing : L.capture}</Text>
                      </Pressable>
                    )}
                    {pickImages && (
                      <Pressable
                        accessibilityRole="button"
                        accessibilityLabel={L.attach}
                        disabled={attachments.length >= MAX_ATTACHMENTS}
                        onPress={onPick}
                        style={styles.action}
                      >
                        <Text style={styles.actionText}>{L.attach}</Text>
                      </Pressable>
                    )}
                  </View>
                )}

                {attachments.length > 0 && (
                  <View style={styles.previews}>
                    {attachments.map((a, i) => (
                      <View key={`${a.uri}-${i}`} style={styles.preview}>
                        <Image source={{ uri: a.uri }} style={styles.previewImage} />
                        <Pressable
                          accessibilityRole="button"
                          accessibilityLabel={L.remove_attachment}
                          hitSlop={8}
                          onPress={() => setAttachments((prev) => prev.filter((_, j) => j !== i))}
                          style={styles.previewRemove}
                        >
                          <Text style={styles.previewRemoveText}>×</Text>
                        </Pressable>
                      </View>
                    ))}
                  </View>
                )}

                <Text style={styles.meta} numberOfLines={1}>
                  {Platform.OS}
                  {path ? ` · ${path}` : ''}
                  {Object.keys(openContext).length ? ` · ${L.context_attached}` : ''}
                </Text>

                <Pressable
                  accessibilityRole="button"
                  accessibilityState={{ disabled: !canSend, busy: status === 'loading' }}
                  disabled={!canSend}
                  onPress={onSubmit}
                  style={[styles.submit, !canSend && styles.submitDisabled]}
                >
                  <Text style={[styles.submitText, !canSend && styles.submitTextDisabled]}>
                    {status === 'loading' ? L.sending : L.send}
                  </Text>
                </Pressable>
              </ScrollView>
            )}
          </View>
        </KeyboardAvoidingView>
      </Modal>
    </LabelsContext.Provider>
  );
}

function ChatIcon({ color }: { color: string }) {
  return (
    <View accessible={false} style={iconStyles.box}>
      <View style={[iconStyles.bubble, { borderColor: color }]} />
      <View style={[iconStyles.tail, { borderTopColor: color }]} />
    </View>
  );
}

const iconStyles = StyleSheet.create({
  box: { width: 16, height: 16 },
  bubble: { width: 16, height: 12, borderRadius: 4, borderWidth: 2 },
  tail: {
    position: 'absolute',
    left: 3,
    top: 11,
    width: 0,
    height: 0,
    borderLeftWidth: 0,
    borderRightWidth: 5,
    borderTopWidth: 5,
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
  },
});

export interface FeedbackButtonProps {
  label?: string;
  options?: OpenFeedbackOptions;
  style?: StyleProp<ViewStyle>;
  children?: ReactNode;
}

export function FeedbackButton({ label, options, style, children }: FeedbackButtonProps) {
  const L = useContext(LabelsContext);
  const text = label ?? L?.title ?? 'Feedback';
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={text} onPress={() => openFeedback(options)} style={style}>
      {children ?? <Text>{text}</Text>}
    </Pressable>
  );
}

function makeStyles(p: Palette) {
  return StyleSheet.create({
    root: { flex: 1 },
    launcher: { position: 'absolute', gap: 8 },
    fab: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      paddingHorizontal: 14,
      paddingVertical: 10,
      borderRadius: 999,
      backgroundColor: p.bg,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: p.border,
      shadowColor: '#000',
      shadowOpacity: 0.15,
      shadowRadius: 8,
      shadowOffset: { width: 0, height: 2 },
      elevation: 4,
    },
    fabNudging: { borderColor: p.accent, borderWidth: 2 },
    pressed: { opacity: 0.8 },
    fabText: { color: p.fg, fontSize: 14, fontWeight: '600' },
    fabHide: {
      position: 'absolute',
      top: -6,
      width: 20,
      height: 20,
      borderRadius: 10,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: p.input,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: p.border,
    },
    fabHideText: { color: p.muted, fontSize: 14, lineHeight: 16 },
    tab: {
      position: 'absolute',
      paddingVertical: 10,
      paddingHorizontal: 8,
      backgroundColor: p.bg,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: p.border,
      elevation: 4,
      shadowColor: '#000',
      shadowOpacity: 0.12,
      shadowRadius: 6,
      shadowOffset: { width: 0, height: 2 },
    },
    tabLeft: { left: 0, borderTopRightRadius: 12, borderBottomRightRadius: 12 },
    tabRight: { right: 0, borderTopLeftRadius: 12, borderBottomLeftRadius: 12 },
    nudge: {
      paddingHorizontal: 12,
      paddingVertical: 8,
      borderRadius: 10,
      backgroundColor: p.accent,
      maxWidth: 240,
    },
    nudgeText: { color: p.accentFg, fontSize: 13, fontWeight: '500' },
    modalRoot: { flex: 1, justifyContent: 'flex-end' },
    backdrop: { ...StyleSheet.absoluteFillObject, backgroundColor: p.backdrop },
    sheet: {
      backgroundColor: p.panel,
      borderTopLeftRadius: 16,
      borderTopRightRadius: 16,
      maxHeight: '90%',
    },
    head: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'space-between',
      paddingHorizontal: 16,
      paddingVertical: 14,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: p.border,
    },
    headTitle: { color: p.fg, fontSize: 16, fontWeight: '600' },
    closeText: { color: p.muted, fontSize: 24, lineHeight: 26 },
    body: { padding: 16, gap: 12 },
    error: { color: p.danger, fontSize: 13 },
    types: { flexDirection: 'row', gap: 8 },
    type: {
      flex: 1,
      paddingVertical: 10,
      borderRadius: 10,
      alignItems: 'center',
      backgroundColor: p.mutedSoft,
      borderWidth: 1,
      borderColor: 'transparent',
    },
    typeActive: { borderColor: p.accent },
    typeText: { color: p.muted, fontSize: 14, fontWeight: '500' },
    typeTextActive: { color: p.fg },
    input: {
      backgroundColor: p.input,
      color: p.fg,
      borderWidth: 1,
      borderColor: p.border,
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 10,
      fontSize: 16,
    },
    textarea: { minHeight: 120 },
    emailHint: { color: p.danger, fontSize: 12, marginTop: -6 },
    actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    action: {
      paddingHorizontal: 12,
      paddingVertical: 8,
      borderRadius: 8,
      backgroundColor: p.mutedSoft,
    },
    actionText: { color: p.fg, fontSize: 13 },
    previews: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    preview: { width: 64, height: 64 },
    previewImage: { width: 64, height: 64, borderRadius: 8, backgroundColor: p.mutedSoft },
    previewRemove: {
      position: 'absolute',
      top: -6,
      right: -6,
      width: 22,
      height: 22,
      borderRadius: 11,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: p.fg,
    },
    previewRemoveText: { color: p.bg, fontSize: 14, lineHeight: 16 },
    meta: { color: p.muted, fontSize: 11 },
    submit: { paddingVertical: 12, borderRadius: 10, alignItems: 'center', backgroundColor: p.accent },
    submitDisabled: { backgroundColor: p.disabled },
    submitText: { color: p.accentFg, fontSize: 15, fontWeight: '600' },
    submitTextDisabled: { color: p.disabledFg },
    thanks: { color: p.muted, fontSize: 14, textAlign: 'center', paddingVertical: 32 },
  });
}
