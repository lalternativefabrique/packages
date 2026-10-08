import { useEffect } from 'react';
import { init } from '@lalternative/nakoda-sdk-browser';
import type { NakodaOptions } from '@lalternative/nakoda-sdk-browser';

export { getSource } from '@lalternative/nakoda-sdk-browser';
export type { NakodaOptions, VisitSource } from '@lalternative/nakoda-sdk-browser';

/**
 * Counts the app's visitors with nakoda, which knows the app by the page's
 * domain; put it once in the root layout.
 */
export function NakodaAnalytics({ endpoint }: NakodaOptions = {}): null {
  useEffect(() => init({ endpoint }), [endpoint]);
  return null;
}
