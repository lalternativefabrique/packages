import { useEffect } from 'react';
import { init } from '@lalternative/nakoda-sdk-browser';
import type { NakodaOptions } from '@lalternative/nakoda-sdk-browser';

export { getSource } from '@lalternative/nakoda-sdk-browser';
export type { NakodaOptions, VisitSource } from '@lalternative/nakoda-sdk-browser';

/** Counts the app's visitors with nakoda; put it once in the root layout. */
export function NakodaAnalytics({ site, endpoint }: NakodaOptions): null {
  useEffect(() => init({ site, endpoint }), [site, endpoint]);
  return null;
}
