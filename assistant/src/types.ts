import type { PointerEvent, ReactNode } from "react";

export interface AssistantScope {
  id: string;
  label: string;
}

export interface AssistantScopePicker {
  scopes: AssistantScope[];
  scopeId: string;
  label: string;
  onChange: (scopeId: string) => void;
}

export interface AssistantQuickAction {
  id: string;
  label: string;
  hint?: string;
  prompt: string;
  options?: Record<string, unknown>;
}

export interface AssistantAttachment {
  id: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  url?: string;
  sourceUrl?: string;
  sourceTitle?: string;
}

export interface AssistantSource {
  title: string;
  url: string;
  excerpt?: string;
  site?: string;
}

export type AssistantErrorCode = "reply_in_progress" | "connection_lost" | "nothing_to_stream";

// Thrown by an adapter's stream/resume so the panel can tell a reply still
// being written, a dropped connection and an empty stream from a failure.
export class AssistantStreamError extends Error {
  readonly code: AssistantErrorCode;
  constructor(code: AssistantErrorCode, message?: string) {
    super(message ?? code);
    this.name = "AssistantStreamError";
    this.code = code;
  }
}

export type ReasoningEffort = "high" | "medium" | "low" | "none";

export interface AssistantReasoning {
  options: ReasoningEffort[];
  default?: ReasoningEffort;
  display?: "gauge" | "select";
}

export interface AssistantSelection {
  text: string;
  before?: string;
  after?: string;
}

export interface AssistantTurnContext {
  thread: AssistantMessage[];
  pending: boolean;
}

export interface AssistantComposer {
  insert(text: string): void;
  focus(): void;
}

export interface AssistantToolCall {
  id: string;
  name: string;
  label?: string;
  args?: unknown;
  ok?: boolean;
  summary?: string;
  meta?: unknown;
  startedAt: number;
  endedAt?: number;
}

export type AssistantEvent =
  | { type: "text"; delta: string }
  | { type: "reasoning"; delta: string }
  | { type: "tool_start"; id: string; name: string; label?: string; args?: unknown }
  | { type: "tool_end"; id: string; ok: boolean; summary?: string; meta?: unknown }
  | { type: "meta"; meta: unknown }
  | { type: "step"; step: number }
  | { type: "sources"; sources: AssistantSource[] }
  | { type: "suggestions"; suggestions: string[] }
  | { type: "attachment"; attachment: AssistantAttachment }
  | { type: "guide"; steps: AssistantGuideStep[] };

export interface AssistantGuideStep {
  field: string;
  note: string;
}

export interface AssistantMessage {
  role: "user" | "assistant";
  content: string;
  attachments?: AssistantAttachment[];
  meta?: unknown;
  reasoning?: string;
  tools?: AssistantToolCall[];
  selection?: AssistantSelection;
  streaming?: boolean;
  stopped?: boolean;
  step?: number;
  sources?: AssistantSource[];
  suggestions?: string[];
  guide?: AssistantGuideStep[];
}

export interface AssistantConversationSummary {
  id: string;
  title: string;
  scopeId?: string;
  updatedAt?: string;
}

export interface AssistantConversation {
  id: string;
  scopeId?: string;
  messages: AssistantMessage[];
  live?: boolean;
}

export interface AssistantScreenField {
  ref: string;
  label: string;
  type: string;
  value?: string;
  required?: boolean;
  protected?: boolean;
  options?: string[];
}

export interface AssistantScreen {
  path: string;
  title: string;
  text: string;
  fields?: AssistantScreenField[];
}

export interface AssistantSendInput {
  scopeId: string;
  conversationId?: string;
  message: string;
  attachmentIds: string[];
  screen?: AssistantScreen;
  reasoningEffort?: ReasoningEffort;
  selection?: AssistantSelection;
  extra?: Record<string, unknown>;
}

export interface AssistantSendResult {
  conversationId: string;
  answer: string;
  meta?: unknown;
  sources?: AssistantSource[];
  suggestions?: string[];
  attachments?: AssistantAttachment[];
}

export interface AttachmentLimits {
  maxCount: number;
  maxImageBytes: number;
  maxFileBytes: number;
  imageTypes: string[];
  fileTypes: string[];
}

export interface AssistantApi {
  send(input: AssistantSendInput): Promise<AssistantSendResult>;
  stream?(input: AssistantSendInput, emit: (event: AssistantEvent) => void, signal: AbortSignal): Promise<AssistantSendResult>;
  listConversations(): Promise<AssistantConversationSummary[]>;
  getConversation(id: string): Promise<AssistantConversation>;
  attach?(file: File, scopeId: string): Promise<AssistantAttachment>;
  stop?(conversationId: string): Promise<void>;
  resume?(conversationId: string, emit: (event: AssistantEvent) => void, signal: AbortSignal): Promise<AssistantSendResult>;
}

export interface AssistantMessageAction {
  id: string;
  label: string;
  icon?: ReactNode;
  when?: (message: AssistantMessage) => boolean;
  onSelect: (message: AssistantMessage) => void;
}

export interface AssistantComposerOption {
  id: string;
  label: string;
  hint?: string;
  checked: boolean;
  onToggle: (checked: boolean) => void;
}

export interface AssistantHistoryContext {
  currentId?: string;
  open: (conversationId: string) => void;
  newConversation: () => void;
}

export interface AssistantLabels {
  minimize: string;
  minimizeAll: string;
  restoreAll: string;
  detach: string;
  dock: string;
  newAssistant: string;
  dockRegion: string;
  running: string;
  ready: string;
  untitled: string;
  moveWindow: string;
  launcher: string;
  history: string;
  newConversation: string;
  recent: string;
  allHistory: string;
  emptyTitle: string;
  intro: string;
  noConversation: string;
  placeholder: string;
  send: string;
  attach: string;
  dictate: string;
  stopDictation: string;
  transcribing: string;
  micDenied: string;
  micMissing: string;
  transcribeFailed: string;
  removeAttachment: string;
  close: string;
  expand: string;
  collapse: string;
  resize: string;
  scope: string;
  loading: string;
  thinking: string;
  dropHere: string;
  uploading: string;
  tooManyFiles: (max: number) => string;
  fileTooLarge: (name: string, max: string) => string;
  fileTypeRefused: (name: string) => string;
  uploadFailed: string;
  requestFailed: string;
  reasoning: string;
  reasoningDefault: string;
  reasoningLevels: Record<ReasoningEffort, string>;
  reasoningHints: Record<ReasoningEffort, string>;
  reasoningShown: string;
  stop: string;
  stopped: string;
  toolsDone: (count: number, seconds: number) => string;
  removeSelection: string;
  replyInProgress: string;
  connectionLost: string;
  resume: string;
  step: (n: number) => string;
  options: string;
  removeOption: (label: string) => string;
  sources: string;
  expandEditor: string;
  collapseEditor: string;
  model: string;
  commands: string;
  recentCommands: string;
  modeCommand: string;
  modeShell: string;
  modeNoShell: string;
  placeholderShell: string;
  hintPrompt: string;
  hintPromptShell: string;
  hintSlash: string;
  hintCommand: string;
  hintShell: (cwd?: string) => string;
  hintRunning: string;
  shellUnavailable: string;
  run: string;
  shellRunning: string;
  shellExit: (code: number | null, seconds?: string) => string;
  shellKilled: string;
  shellTruncated: string;
  shellPending: string;
  shellOpenTerminal: string;
  shellCollapse: string;
  shellExpand: string;
  shellDismiss: string;
  guideShow: string;
  guideTitle: string;
  guidePrevious: string;
  guideNext: string;
  guideDone: string;
}

export interface AssistantPanelProps {
  api: AssistantApi;
  scopes: AssistantScope[];
  scopeId: string;
  onScopeChange?: (scopeId: string) => void;
  scopeLocked?: boolean;
  quickActions?: AssistantQuickAction[];
  attachmentLimits?: AttachmentLimits;
  pasteAsFileThreshold?: number;
  labels?: Partial<AssistantLabels>;
  renderMarkdown?: (text: string, message: AssistantMessage) => ReactNode;
  renderToolCall?: (tool: AssistantToolCall, message: AssistantMessage, turn: AssistantTurnContext) => ReactNode | undefined;
  renderComposerAccessory?: (composer: AssistantComposer) => ReactNode;
  banner?: ReactNode;
  ask?: { id: string; text: string; shownAs?: string; extra?: Record<string, unknown>; fresh?: boolean; silent?: boolean };
  draftSeed?: { id: string; text: string; fresh?: boolean };
  sendExtra?: Record<string, unknown>;
  historyRail?: boolean;
  renderHistory?: (history: AssistantHistoryContext) => ReactNode;
  aboveComposer?: ReactNode;
  subtitle?: ReactNode;
  emptyState?: ReactNode;
  agentName?: string;
  messageActions?: AssistantMessageAction[];
  onApplySuggestion?: (message: AssistantMessage, suggestion: string) => void;
  composerOptions?: AssistantComposerOption[];
  composerOverlay?: ReactNode;
  transcribe?: AssistantTranscribe;
  dictationLanguage?: string;
  onComposerEscape?: () => boolean;
  commands?: { name: string; description?: string; prefill?: boolean }[];
  onCommand?: (command: { name: string; description?: string; prefill?: boolean }) => void;
  composerHistory?: string[];
  shellElsewhere?: boolean;
  onComposerHistoryChange?: (history: string[]) => void;
  models?: { options: { id: string; label: string }[]; value: string; onChange: (id: string) => void };
  singleConversation?: boolean;
  showSources?: boolean;
  disabledReason?: string;
  bodyOverride?: ReactNode;
  renderScopePicker?: (picker: AssistantScopePicker) => ReactNode;
  renderMessageFooter?: (message: AssistantMessage) => ReactNode;
  readScreen?: () => AssistantScreen | undefined;
  onGuide?: (steps: AssistantGuideStep[]) => void;
  widthStorageKey?: string;
  onClose: () => void;
  initialHistory?: boolean;
  reasoning?: AssistantReasoning;
  initialSelection?: AssistantSelection;
  embedded?: boolean;
  initialConversationId?: string;
  headerActions?: ReactNode;
  onHeaderPointerDown?: (e: PointerEvent<HTMLElement>) => void;
  onRunningChange?: (running: boolean) => void;
  onConversationChange?: (conversationId: string, title: string) => void;
  onAnswered?: () => void;
}

export type AssistantTranscribe = (audio: Blob, language: string) => Promise<string | { notice: string }>;
