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
}

export interface AssistantAttachment {
  id: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
}

export type ReasoningEffort = "high" | "medium" | "low" | "none";

export interface AssistantReasoning {
  options: ReasoningEffort[];
  default?: ReasoningEffort;
}

export interface AssistantSelection {
  text: string;
  before?: string;
  after?: string;
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
  | { type: "meta"; meta: unknown };

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
}

export interface AssistantScreen {
  path: string;
  title: string;
  text: string;
}

export interface AssistantSendInput {
  scopeId: string;
  conversationId?: string;
  message: string;
  attachmentIds: string[];
  screen?: AssistantScreen;
  reasoningEffort?: ReasoningEffort;
  selection?: AssistantSelection;
}

export interface AssistantSendResult {
  conversationId: string;
  answer: string;
  meta?: unknown;
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
  reasoningShown: string;
  stop: string;
  stopped: string;
  toolsDone: (count: number, seconds: number) => string;
  removeSelection: string;
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
  renderMarkdown?: (text: string) => ReactNode;
  renderScopePicker?: (picker: AssistantScopePicker) => ReactNode;
  renderMessageFooter?: (message: AssistantMessage) => ReactNode;
  readScreen?: () => AssistantScreen | undefined;
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
