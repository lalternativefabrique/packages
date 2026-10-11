export type FeedbackType = 'bug' | 'idea' | 'other';

export type FeedbackSource = 'manual' | 'error' | 'nudge';

export interface OpenFeedbackOptions {
  type?: FeedbackType;
  message?: string;
  source?: FeedbackSource;
  context?: Record<string, string>;
}

type FeedbackEvent = { kind: 'open'; options: OpenFeedbackOptions } | { kind: 'nudge' };

const listeners = new Set<(event: FeedbackEvent) => void>();

export function subscribe(listener: (event: FeedbackEvent) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function openFeedback(options: OpenFeedbackOptions = {}): void {
  for (const listener of listeners) listener({ kind: 'open', options });
}

export function reportProblem(error: unknown, context: Record<string, string> = {}): void {
  const err = error instanceof Error ? error : null;
  openFeedback({
    type: 'bug',
    source: 'error',
    context: {
      ...context,
      error: err ? `${err.name}: ${err.message}` : String(error),
      ...(err?.stack ? { stack: err.stack } : {}),
    },
  });
}

export function nudgeFeedback(): void {
  for (const listener of listeners) listener({ kind: 'nudge' });
}
