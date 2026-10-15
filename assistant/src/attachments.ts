import { useCallback, useEffect, useRef, useState } from "react";
import { formatSize } from "./format";
import type { AssistantAttachment, AssistantLabels, AttachmentLimits } from "./types";

export interface PendingAttachment {
  localId: string;
  file: File;
  status: "uploading" | "done" | "error";
  error?: string;
  attachment?: AssistantAttachment;
  previewUrl?: string;
}

const typesByExtension: Record<string, string> = {
  txt: "text/plain",
  log: "text/plain",
  md: "text/markdown",
  markdown: "text/markdown",
  csv: "text/csv",
  json: "application/json",
  xml: "application/xml",
  yaml: "application/yaml",
  yml: "application/yaml",
  html: "text/html",
};

export function withType(file: File): File {
  if (file.type) return file;
  const type = typesByExtension[file.name.split(".").pop()?.toLowerCase() ?? ""];
  return type ? new File([file], file.name, { type }) : file;
}

export function fileError(file: File, limits: AttachmentLimits | undefined, labels: AssistantLabels): string | undefined {
  if (!limits) return undefined;
  const type = file.type.toLowerCase();
  if (limits.imageTypes.includes(type)) {
    return file.size > limits.maxImageBytes ? labels.fileTooLarge(file.name, formatSize(limits.maxImageBytes)) : undefined;
  }
  if (limits.fileTypes.includes(type)) {
    return file.size > limits.maxFileBytes ? labels.fileTooLarge(file.name, formatSize(limits.maxFileBytes)) : undefined;
  }
  return labels.fileTypeRefused(file.name);
}

export function pastedFiles(data: DataTransfer | null, pasteAsFileThreshold: number | undefined, name: string): File[] {
  if (!data) return [];
  const files = Array.from(data.items ?? [])
    .filter((item) => item.kind === "file")
    .map((item) => item.getAsFile())
    .filter((f): f is File => f !== null);
  if (files.length > 0) return files;
  const text = data.getData("text/plain");
  if (pasteAsFileThreshold !== undefined && text.length > pasteAsFileThreshold) {
    return [new File([text], name, { type: "text/plain" })];
  }
  return [];
}

function previewOf(file: File): string | undefined {
  if (!file.type.startsWith("image/") || typeof URL.createObjectURL !== "function") return undefined;
  return URL.createObjectURL(file);
}

function revoke(item: PendingAttachment) {
  if (item.previewUrl && typeof URL.revokeObjectURL === "function") URL.revokeObjectURL(item.previewUrl);
}

let nextId = 0;

export function useAttachments({
  attach,
  scopeId,
  limits,
  labels,
}: {
  attach?: (file: File, scopeId: string) => Promise<AssistantAttachment>;
  scopeId: string;
  limits?: AttachmentLimits;
  labels: AssistantLabels;
}) {
  const [items, setItems] = useState<PendingAttachment[]>([]);
  const [notice, setNotice] = useState<string | null>(null);
  const itemsRef = useRef(items);
  itemsRef.current = items;

  useEffect(() => () => itemsRef.current.forEach(revoke), []);

  const update = (localId: string, patch: Partial<PendingAttachment>) =>
    setItems((list) => list.map((i) => (i.localId === localId ? { ...i, ...patch } : i)));

  const add = useCallback(
    (incoming: File[]) => {
      if (!attach || incoming.length === 0) return;
      setNotice(null);
      const room = limits ? limits.maxCount - itemsRef.current.length : incoming.length;
      if (incoming.length > room) setNotice(labels.tooManyFiles(limits?.maxCount ?? 0));
      const accepted = incoming.slice(0, Math.max(0, room)).map((raw) => {
        const file = withType(raw);
        const error = fileError(file, limits, labels);
        const item: PendingAttachment = {
          localId: `a${nextId++}`,
          file,
          status: error ? "error" : "uploading",
          error,
          previewUrl: error ? undefined : previewOf(file),
        };
        return item;
      });
      if (accepted.length === 0) return;
      itemsRef.current = [...itemsRef.current, ...accepted];
      setItems(itemsRef.current);
      for (const item of accepted) {
        if (item.status !== "uploading") continue;
        attach(item.file, scopeId).then(
          (attachment) => update(item.localId, { status: "done", attachment }),
          (err: unknown) =>
            update(item.localId, { status: "error", error: (err as { message?: string })?.message || labels.uploadFailed }),
        );
      }
    },
    [attach, scopeId, limits, labels],
  );

  const remove = useCallback((localId: string) => {
    setItems((list) => {
      list.filter((i) => i.localId === localId).forEach(revoke);
      return list.filter((i) => i.localId !== localId);
    });
    setNotice(null);
  }, []);

  const clear = useCallback(() => {
    itemsRef.current.forEach(revoke);
    itemsRef.current = [];
    setItems([]);
    setNotice(null);
  }, []);

  const done = items.filter((i) => i.status === "done" && i.attachment).map((i) => i.attachment as AssistantAttachment);
  return {
    enabled: !!attach,
    items,
    notice,
    done,
    uploading: items.some((i) => i.status === "uploading"),
    add,
    remove,
    clear,
  };
}
