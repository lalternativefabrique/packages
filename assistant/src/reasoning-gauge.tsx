import type { KeyboardEvent } from "react";
import type { AssistantLabels, ReasoningEffort } from "./types";

const ASCENDING: ReasoningEffort[] = ["none", "low", "medium", "high"];

export function ReasoningGauge({
  options,
  fallback,
  value,
  onChange,
  labels,
}: {
  options: ReasoningEffort[];
  fallback?: ReasoningEffort;
  value: ReasoningEffort | undefined;
  onChange: (value: ReasoningEffort | undefined) => void;
  labels: AssistantLabels;
}) {
  const steps = ASCENDING.filter((level) => options.includes(level));
  const shown = value ?? fallback;
  const index = shown ? steps.indexOf(shown) : -1;
  const label = shown ? labels.reasoningLevels[shown] : labels.reasoningDefault;
  const text = value ? label : fallback ? `${labels.reasoningDefault} · ${label}` : labels.reasoningDefault;
  const title = `${labels.reasoning} : ${text}${shown ? ` — ${labels.reasoningHints[shown]}` : ""}`;

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "ArrowRight" || e.key === "ArrowUp") {
      e.preventDefault();
      onChange(steps[Math.min(steps.length - 1, index + 1)]);
    } else if (e.key === "ArrowLeft" || e.key === "ArrowDown") {
      e.preventDefault();
      onChange(steps[Math.max(0, index - 1)]);
    } else if (e.key === "Home") {
      e.preventDefault();
      onChange(steps[0]);
    } else if (e.key === "End") {
      e.preventDefault();
      onChange(steps[steps.length - 1]);
    } else if (e.key === "Escape" || e.key === "Backspace" || e.key === "Delete") {
      if (value === undefined) return;
      e.preventDefault();
      e.stopPropagation();
      onChange(undefined);
    }
  };

  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={labels.reasoning}
      aria-valuemin={0}
      aria-valuemax={steps.length - 1}
      aria-valuenow={Math.max(0, index)}
      aria-valuetext={text}
      title={title}
      data-default={value === undefined || undefined}
      className="lalt-assistant-gauge"
      onKeyDown={onKeyDown}
    >
      {steps.map((level, i) => (
        <button
          key={level}
          type="button"
          tabIndex={-1}
          aria-hidden="true"
          data-filled={i <= index || undefined}
          style={{ height: `${6 + (i * 10) / Math.max(1, steps.length - 1)}px` }}
          className="lalt-assistant-gauge-bar"
          onClick={() => onChange(value === level ? undefined : level)}
        />
      ))}
    </div>
  );
}
