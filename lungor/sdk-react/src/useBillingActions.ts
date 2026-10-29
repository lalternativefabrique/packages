import { useCallback, useEffect, useState } from 'react';
import type { PricingPlan } from './plans.js';

/** All a provider reveals about the card on file. */
export interface BillingCard {
  brand: string;
  last4: string;
  expiresAt?: string;
}

/** What an upgrade will cost, shown before anything is charged. */
export interface PlanChangeQuote {
  plan: PricingPlan;
  nowCents: number;
  recurringCents: number;
  card?: BillingCard;
}

export type BillingAction = 'change' | 'cancel' | 'resume' | 'withdraw' | 'card';

/**
 * Why an action failed. `refused` is Lungor saying no to this request (a
 * declined card, a period already over); `unavailable` is worth retrying.
 */
export type BillingFailureReason = 'refused' | 'no_subscription' | 'unavailable';

export interface BillingActionFailure {
  action: BillingAction;
  reason: BillingFailureReason;
}

/** The subscription-management state and actions BillingPage renders. */
export interface BillingActions {
  card?: BillingCard;
  busy: boolean;
  failure?: BillingActionFailure;
  quote?: PlanChangeQuote;
  confirmingCancel: boolean;
  changePlan: (plan: PricingPlan) => void;
  confirmPlanChange: () => void;
  dismissQuote: () => void;
  askCancel: () => void;
  confirmCancel: () => void;
  dismissCancel: () => void;
  resume: () => void;
  withdrawPending: () => void;
  replaceCard: () => void;
}

export interface UseBillingActionsOptions {
  /** Where the app mounted sdk-go's billinghttp handler, e.g. `/api/billing/manage`. */
  basePath: string;
  /**
   * Sends the app's own credentials. Defaults to fetch with same-origin
   * cookies. Pass a stable function: the card is re-read when it changes.
   */
  fetch?: typeof fetch;
  /** Called after a change Lungor applied: refresh the subscription. */
  onChanged?: () => void;
  /** False skips reading the card, for a user with nothing paid. Defaults to true. */
  enabled?: boolean;
  /**
   * Sends the user to the provider's card page. Defaults to replacing the
   * current page; a desktop shell opens it in the system browser instead.
   */
  redirect?: (url: string) => void;
}

class ActionError extends Error {
  constructor(readonly reason: BillingFailureReason) {
    super(reason);
  }
}

function failureOf(res: Response): BillingFailureReason {
  if (res.status === 404) return 'no_subscription';
  if (res.status === 400 || res.status === 409 || res.status === 422) return 'refused';
  return 'unavailable';
}

function expectOk(res: Response): void {
  if (!res.ok) throw new ActionError(failureOf(res));
}

interface ChangePlanResponse {
  consent_required: boolean;
  consent_amount_cents?: number;
  consent_recurring_cents?: number;
  card?: { brand: string; last4: string; expires_at?: string };
}

const sameOriginFetch: typeof fetch = (input, init) =>
  fetch(input, { credentials: 'same-origin', ...init });

function toCard(c?: { brand: string; last4: string; expires_at?: string }): BillingCard | undefined {
  return c?.last4 ? { brand: c.brand, last4: c.last4, expiresAt: c.expires_at } : undefined;
}

/**
 * Drives the actions that change an existing subscription against the routes
 * of sdk-go's billinghttp handler. Every rule (prorata, consent, which card,
 * when a downgrade applies) is Lungor's; this only sequences the calls.
 */
export function useBillingActions({
  basePath,
  fetch: send = sameOriginFetch,
  onChanged,
  enabled = true,
  redirect = (url) => window.location.assign(url),
}: UseBillingActionsOptions): BillingActions {
  const base = basePath.replace(/\/$/, '');
  const [card, setCard] = useState<BillingCard>();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<BillingActionFailure>();
  const [quote, setQuote] = useState<PlanChangeQuote>();
  const [confirmingCancel, setConfirmingCancel] = useState(false);

  const loadCard = useCallback(async () => {
    if (!enabled) {
      setCard(undefined);
      return;
    }
    try {
      const res = await send(`${base}/payment-method`);
      setCard(res.ok ? toCard(await res.json()) : undefined);
    } catch {
      setCard(undefined);
    }
  }, [base, send, enabled]);

  useEffect(() => {
    void loadCard();
  }, [loadCard]);

  const run = useCallback(async (action: BillingAction, call: () => Promise<void>) => {
    setBusy(true);
    setFailure(undefined);
    try {
      await call();
    } catch (err) {
      setFailure({ action, reason: err instanceof ActionError ? err.reason : 'unavailable' });
    } finally {
      setBusy(false);
    }
  }, []);

  const applied = useCallback(() => {
    onChanged?.();
    void loadCard();
  }, [onChanged, loadCard]);

  const post = useCallback(
    (path: string, body?: unknown) =>
      send(`${base}${path}`, {
        method: 'POST',
        headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      }),
    [base, send],
  );

  const requestChange = useCallback(
    (plan: PricingPlan, agreedCents?: number) =>
      run('change', async () => {
        const res = await post('/change-plan', {
          code: plan.code,
          agreed: agreedCents !== undefined,
          agreed_amount_cents: agreedCents,
        });
        expectOk(res);
        const out = (await res.json()) as ChangePlanResponse;
        if (out.consent_required) {
          setQuote({
            plan,
            nowCents: out.consent_amount_cents ?? 0,
            recurringCents: out.consent_recurring_cents ?? plan.amount,
            card: toCard(out.card),
          });
          return;
        }
        setQuote(undefined);
        applied();
      }),
    [run, post, applied],
  );

  const simple = useCallback(
    (action: BillingAction, path: string, after?: () => void) =>
      run(action, async () => {
        expectOk(await post(path));
        after?.();
        applied();
      }),
    [run, post, applied],
  );

  return {
    card,
    busy,
    failure,
    quote,
    confirmingCancel,
    changePlan: (plan) => void requestChange(plan),
    confirmPlanChange: () => quote && void requestChange(quote.plan, quote.nowCents),
    dismissQuote: () => setQuote(undefined),
    askCancel: () => setConfirmingCancel(true),
    confirmCancel: () => void simple('cancel', '/cancel', () => setConfirmingCancel(false)),
    dismissCancel: () => setConfirmingCancel(false),
    resume: () => void simple('resume', '/resume'),
    withdrawPending: () => void simple('withdraw', '/withdraw-pending'),
    replaceCard: () =>
      void run('card', async () => {
        const res = await post('/payment-method');
        expectOk(res);
        const { redirect_url: url } = (await res.json()) as { redirect_url?: string };
        if (!url) throw new ActionError('unavailable');
        redirect(url);
      }),
  };
}
