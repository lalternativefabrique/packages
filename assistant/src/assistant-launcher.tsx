import { useState } from "react";
import { AssistantPanel } from "./assistant-panel";
import { Icon } from "./icons";
import { defaultLabels } from "./labels";
import type { AssistantPanelProps } from "./types";

export type AssistantLauncherProps = Omit<AssistantPanelProps, "onClose" | "initialHistory">;

export function AssistantLauncher(props: AssistantLauncherProps) {
  const [open, setOpen] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const labels = { ...defaultLabels, ...props.labels };
  const openPanel = (history: boolean) => {
    setShowHistory(history);
    setOpen(true);
  };
  if (open) return <AssistantPanel {...props} initialHistory={showHistory} onClose={() => setOpen(false)} />;
  return (
    <div className="lalt-assistant lalt-assistant-launcher">
      <button type="button" onClick={() => openPanel(true)} className="lalt-assistant-launch lalt-assistant-launch-secondary">
        <Icon name="clock" />
        {labels.history}
      </button>
      <button type="button" onClick={() => openPanel(false)} className="lalt-assistant-launch lalt-assistant-launch-primary">
        <Icon name="bot" />
        {labels.launcher}
      </button>
    </div>
  );
}
