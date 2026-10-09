import { useEffect, useRef } from 'react';
import type { SkalpaiFeedbackElement } from '@lalternative/skalpai-feedback-widget';
import '@lalternative/skalpai-feedback-widget';

export type FeedbackTheme = 'light' | 'dark' | 'auto';

export type FeedbackPlacement = 'bottom-left' | 'bottom-right' | 'inline';

export type FeedbackLabels = {
  title?: string;
  send?: string;
  sending?: string;
  close?: string;
  bug?: string;
  idea?: string;
  other?: string;
  placeholder?: string;
  thanks?: string;
  capture?: string;
  capturing?: string;
  remove_screenshot?: string;
  attach?: string;
  remove_attachment?: string;
  email_placeholder?: string;
  email_invalid?: string;
  hide?: string;
  show?: string;
  nudge?: string;
  context_attached?: string;
};

export interface FeedbackButtonProps {
  /** Skalpai backend URL, e.g. `https://api.skalpai.com` */
  endpoint: string;
  /** Project API key (creates a feedback scoped to that project) */
  apiKey: string;
  /** Project UUID (used in the URL: /api/projects/:projectId/issues/submit) */
  projectId: string;
  /** Optional user identifier (email, user id…) attached to the feedback */
  userIdentifier?: string;
  /** Override theme detection. Default: 'auto' (Tailwind/shadcn/Mantine/system) */
  theme?: FeedbackTheme;
  /** Built-in label set. Default: 'fr' */
  lang?: string;
  /** Label overrides on top of the `lang` set */
  labels?: FeedbackLabels;
  /**
   * Where the launcher sits. Default: 'bottom-left' (floating). Use 'inline' to
   * drop it into the host layout — the launcher stops being fixed, so it cannot
   * overlap the surrounding chrome. Fine-tune the floating insets with the
   * `--skalpai-fab-inset-block` / `--skalpai-fab-inset-inline` custom properties.
   */
  placement?: FeedbackPlacement;
  /**
   * Start with the launcher folded into an edge tab. Visitors can fold it
   * themselves with the launcher's close affordance; that choice is remembered
   * per project in localStorage and wins over this prop on later visits.
   */
  collapsed?: boolean;
  /** Fired whenever the launcher folds or unfolds, by the visitor or the host. */
  onCollapseChange?: (collapsed: boolean) => void;
}

export function FeedbackButton({
  endpoint,
  apiKey,
  projectId,
  userIdentifier,
  theme,
  lang,
  labels,
  placement,
  collapsed,
  onCollapseChange,
}: FeedbackButtonProps) {
  const ref = useRef<SkalpaiFeedbackElement | null>(null);

  useEffect(() => {
    if (ref.current && userIdentifier !== undefined) {
      ref.current.userIdentifier = userIdentifier;
    }
  }, [userIdentifier]);

  useEffect(() => {
    if (ref.current && collapsed !== undefined) ref.current.collapsed = collapsed;
  }, [collapsed]);

  useEffect(() => {
    const el = ref.current;
    if (!el || !onCollapseChange) return;
    const handler = (e: Event) => onCollapseChange((e as CustomEvent<{ collapsed: boolean }>).detail.collapsed);
    el.addEventListener('skalpai-feedback-collapse', handler);
    return () => el.removeEventListener('skalpai-feedback-collapse', handler);
  }, [onCollapseChange]);

  if (!endpoint || !apiKey || !projectId) return null;

  const labelsAttr = labels ? JSON.stringify(labels) : undefined;

  return (
    <skalpai-feedback
      ref={ref}
      endpoint={endpoint}
      api-key={apiKey}
      project-id={projectId}
      {...(theme && theme !== 'auto' ? { theme } : {})}
      {...(lang ? { lang } : {})}
      {...(labelsAttr ? { labels: labelsAttr } : {})}
      {...(placement && placement !== 'bottom-left' ? { placement } : {})}
      {...(collapsed ? { collapsed: '' } : {})}
    />
  );
}

declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      'skalpai-feedback': React.DetailedHTMLProps<
        React.HTMLAttributes<HTMLElement> & {
          'api-key'?: string;
          endpoint?: string;
          'project-id'?: string;
          theme?: 'light' | 'dark';
          labels?: string;
          lang?: string;
          placement?: FeedbackPlacement;
          collapsed?: string;
        },
        SkalpaiFeedbackElement
      >;
    }
  }
}
