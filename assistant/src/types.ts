import type { ReactNode } from "react";

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

export interface AssistantMessage {
  role: "user" | "assistant";
  content: string;
  attachments?: AssistantAttachment[];
  meta?: unknown;
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
  listConversations(): Promise<AssistantConversationSummary[]>;
  getConversation(id: string): Promise<AssistantConversation>;
  attach?(file: File, scopeId: string): Promise<AssistantAttachment>;
}

export interface AssistantLabels {
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
}
