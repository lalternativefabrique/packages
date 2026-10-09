import { useCallback, type ReactNode } from 'react';
import {
  nudgeFeedback,
  openFeedback,
  reportProblem,
  type OpenFeedbackOptions,
} from '@lalternative/skalpai-feedback-widget';

export function useFeedback() {
  const open = useCallback((options?: OpenFeedbackOptions) => openFeedback(options), []);
  const report = useCallback(
    (error: unknown, context?: Record<string, string>) => reportProblem(error, context),
    [],
  );
  return { open, report, nudge: nudgeFeedback };
}

export interface ReportProblemProps {
  error: unknown;
  context?: Record<string, string>;
  className?: string;
  children?: ReactNode;
}

export function ReportProblem({ error, context, className, children }: ReportProblemProps) {
  return (
    <button type="button" className={className} onClick={() => reportProblem(error, context)}>
      {children ?? 'Signaler ce problème'}
    </button>
  );
}
