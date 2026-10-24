import { useEffect, useMemo, useRef, type ReactNode } from 'react';
import { CheckoutOutcome, type CheckoutOutcomeLabels } from './CheckoutOutcome.js';
import { readCheckoutSessionId, type CheckoutSession } from './checkout.js';
import {
  formatPrice,
  intervalSuffix,
  isFreePlan,
  type PricingAllocation,
  type PricingPlan,
} from './plans.js';
import { PricingTable, type PricingIntent, type PricingTableLabels } from './PricingTable.js';

/**
 * The route every app mounts its billing page on, and the one a checkout
 * returns to. One name across products: the page that reads how a checkout
 * ended is the same page everywhere, and a return URL is built from this
 * rather than from each app's own idea of where its plans live.
 */
export const BILLING_PATH = '/billing';

/** The query parameter the public pricing page hands over after sign-up. */
export const REQUESTED_PLAN_PARAM = 'plan';

/**
 * A subscription as Lungor's `GET /entitlements` reports it. Everything the
 * page shows travels on that read, so the app keeps no row of its own.
 */
export interface BillingSubscription {
  /** Lungor's verdict: may this user be served the paid tier right now. */
  entitled: boolean;
  /** active, trialing, past_due, canceled, unpaid, paused, or no_subscription. */
  status: string;
  planCode?: string;
  currentPeriodStart?: string | Date;
  currentPeriodEnd?: string | Date;
  /** The customer asked to stop: access runs to the period end, no renewal follows. */
  cancelAtPeriodEnd?: boolean;
  /** A smaller plan scheduled for the renewal. */
  pendingPlanCode?: string;
  pendingPlanEffectiveAt?: string | Date;
  /** Remaining allowance per metered unit, when known. */
  balances?: Record<string, number>;
}

export interface BillingPageLabels {
  heading?: string;
  description?: string;
  currentPlan?: string;
  noPlan?: string;
  noPlanDetail?: string;
  renewsOn?: string;
  endsOn?: string;
  /** `{plan}` and `{date}` are substituted. */
  pendingChange?: string;
  withdrawPending?: string;
  cancelScheduled?: string;
  resume?: string;
  cancel?: string;
  pastDue?: string;
  remaining?: string;
  plansHeading?: string;
}

const DEFAULT_LABELS: Required<BillingPageLabels> = {
  heading: 'Abonnement',
  description: 'Votre offre, son renouvellement et les offres disponibles.',
  currentPlan: 'Votre offre actuelle',
  noPlan: 'Aucune offre active',
  noPlanDetail: 'Choisissez une offre ci-dessous pour commencer.',
  renewsOn: 'Renouvellement le {date}',
  endsOn: 'Accès jusqu’au {date}',
  pendingChange: 'Passage à {plan} le {date}',
  withdrawPending: 'Annuler ce changement',
  cancelScheduled: 'Résiliation programmée : aucun renouvellement ne suivra.',
  resume: 'Reprendre l’abonnement',
  cancel: 'Résilier',
  pastDue: 'Le dernier paiement a échoué. Mettez à jour votre moyen de paiement pour conserver votre offre.',
  remaining: 'Il vous reste',
  plansHeading: 'Changer d’offre',
};

export interface BillingPageProps {
  /** Plans from `GET /finance/plans`, in the order Lungor returned them. */
  plans: PricingPlan[];
  /** The user's subscription from `GET /entitlements`. Undefined while loading. */
  subscription?: BillingSubscription;
  /**
   * Reads a checkout session from YOUR backend, which proxies Lungor's
   * `GET /finance/checkout/{session_id}` with the app key.
   */
  fetchSession: (sessionId: string) => Promise<CheckoutSession>;
  /** Opens a checkout for a plan the user does not hold yet. YOUR backend does it. */
  onCheckout: (plan: PricingPlan) => void;
  /** Moves an entitled user to another plan. Omit to always go through checkout. */
  onChangePlan?: (plan: PricingPlan) => void;
  onCancel?: () => void;
  onResume?: () => void;
  onWithdrawPendingPlan?: () => void;
  /** Called once the returned checkout reads as paid: refresh the subscription. */
  onPaid?: (session: CheckoutSession) => void;
  /** Pressed on a confirmed payment: back into the app, typically its home. */
  onContinue?: (session: CheckoutSession) => void;
  /** Leaves an unpaid ending alone: strips the checkout parameters and stays. */
  onLeaveCheckoutReturn?: () => void;
  /**
   * A plan code handed over by the public pricing page (`?plan=`), for which
   * a checkout opens on arrival. Read from the URL when omitted.
   */
  requestedPlanCode?: string | null;
  busy?: boolean;
  locale?: string;
  /** Renders a plan's allowance on the grid (`credit` → "100 crédits par mois"). */
  formatUnit?: (allocation: PricingAllocation) => string;
  /**
   * Renders what remains of a unit on the current plan ("12 crédits"). Kept
   * apart from formatUnit: an allowance reads "per period", a balance does not.
   */
  formatBalance?: (unit: string, amount: number) => string;
  labels?: BillingPageLabels;
  outcomeLabels?: CheckoutOutcomeLabels;
  pricingLabels?: PricingTableLabels;
  /** Shown in the current plan's card under its balances, e.g. the app's own usage meter. */
  currentPlanExtra?: ReactNode;
  className?: string;
}

function fill(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (_, key: string) => values[key] ?? '');
}

function formatDate(value: string | Date | undefined, locale?: string): string {
  if (!value) return '';
  const d = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(d.getTime())) return '';
  try {
    return new Intl.DateTimeFormat(locale, { dateStyle: 'long' }).format(d);
  } catch {
    return d.toISOString().slice(0, 10);
  }
}

function readRequestedPlan(): string | undefined {
  if (typeof window === 'undefined') return undefined;
  return new URLSearchParams(window.location.search).get(REQUESTED_PLAN_PARAM) ?? undefined;
}

/**
 * The billing page every app mounts on BILLING_PATH: how the checkout the
 * user just returned from ended, the plan they hold, and the plans they can
 * move to. The app supplies data and callbacks; the page is the same
 * everywhere.
 */
export function BillingPage({
  plans,
  subscription,
  fetchSession,
  onCheckout,
  onChangePlan,
  onCancel,
  onResume,
  onWithdrawPendingPlan,
  onPaid,
  onContinue,
  onLeaveCheckoutReturn,
  requestedPlanCode,
  busy = false,
  locale,
  formatUnit,
  formatBalance,
  labels,
  outcomeLabels,
  pricingLabels,
  currentPlanExtra,
  className = '',
}: BillingPageProps) {
  const l = { ...DEFAULT_LABELS, ...labels };
  const hasSubscription = Boolean(
    subscription && subscription.status !== 'no_subscription' && subscription.planCode,
  );
  const current = useMemo(
    () => plans.find((p) => p.code === subscription?.planCode),
    [plans, subscription?.planCode],
  );
  const pending = useMemo(
    () => plans.find((p) => p.code === subscription?.pendingPlanCode),
    [plans, subscription?.pendingPlanCode],
  );

  // A plan handed over by the public pricing page opens its checkout once,
  // and only off a checkout return: the two must never race on one page.
  const requested = requestedPlanCode === undefined ? readRequestedPlan() : requestedPlanCode;
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current || !requested || !subscription || readCheckoutSessionId()) return;
    const plan = plans.find((p) => p.code === requested);
    if (!plan || isFreePlan(plan) || plan.code === subscription.planCode) return;
    opened.current = true;
    onCheckout(plan);
  }, [requested, plans, subscription, onCheckout]);

  const select = (plan: PricingPlan, intent: PricingIntent) => {
    if (intent !== 'checkout') return;
    if (hasSubscription && subscription?.entitled && onChangePlan) onChangePlan(plan);
    else onCheckout(plan);
  };

  const periodEnd = formatDate(subscription?.currentPeriodEnd, locale);
  const scheduledOff = Boolean(subscription?.cancelAtPeriodEnd);
  const pastDue = subscription?.status === 'past_due' || subscription?.status === 'unpaid';

  return (
    <div className={`flex flex-col gap-8 ${className}`.trim()}>
      <CheckoutOutcome
        variant="hero"
        fetchSession={fetchSession}
        onPaid={onPaid}
        onContinue={onContinue ?? onLeaveCheckoutReturn}
        onDismiss={onLeaveCheckoutReturn}
        onRetry={() =>
          document.getElementById('lungor-plans')?.scrollIntoView({ behavior: 'smooth' })
        }
        labels={outcomeLabels}
      />

      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-foreground">{l.heading}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{l.description}</p>
      </header>

      <section
        aria-label={l.currentPlan}
        className="flex flex-col gap-4 rounded-xl border border-border bg-card p-6 text-card-foreground"
      >
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {l.currentPlan}
        </p>
        {subscription === undefined ? (
          <div className="h-16 animate-pulse rounded-md bg-muted" />
        ) : !hasSubscription ? (
          <div>
            <p className="text-lg font-semibold text-foreground">{l.noPlan}</p>
            <p className="mt-1 text-sm text-muted-foreground">{l.noPlanDetail}</p>
          </div>
        ) : (
          <>
            <div className="flex flex-wrap items-baseline justify-between gap-3">
              <p className="text-lg font-semibold text-foreground">
                {current?.name ?? subscription.planCode}
              </p>
              {current ? (
                <p className="text-sm text-muted-foreground">
                  {current.priceLabel ??
                    formatPrice(current.amount, current.currency, locale) + intervalSuffix(current)}
                </p>
              ) : null}
            </div>

            {pastDue ? (
              <p className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-foreground">
                {l.pastDue}
              </p>
            ) : null}

            {periodEnd ? (
              <p className="text-sm text-muted-foreground">
                {fill(scheduledOff ? l.endsOn : l.renewsOn, { date: periodEnd })}
              </p>
            ) : null}

            {subscription.balances
              ? Object.entries(subscription.balances).map(([unit, amount]) => (
                  <p key={unit} className="text-sm text-muted-foreground">
                    {l.remaining}{' '}
                    <span className="font-medium text-foreground">
                      {formatBalance ? formatBalance(unit, amount) : `${amount} ${unit}`}
                    </span>
                  </p>
                ))
              : null}

            {currentPlanExtra}

            {subscription.pendingPlanCode ? (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
                <span className="text-foreground">
                  {fill(l.pendingChange, {
                    plan: pending?.name ?? subscription.pendingPlanCode,
                    date: formatDate(subscription.pendingPlanEffectiveAt, locale),
                  })}
                </span>
                {onWithdrawPendingPlan ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={onWithdrawPendingPlan}
                    className="text-sm font-medium text-foreground underline-offset-4 hover:underline disabled:opacity-50"
                  >
                    {l.withdrawPending}
                  </button>
                ) : null}
              </div>
            ) : null}

            {scheduledOff ? (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
                <span className="text-foreground">{l.cancelScheduled}</span>
                {onResume ? (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={onResume}
                    className="text-sm font-medium text-foreground underline-offset-4 hover:underline disabled:opacity-50"
                  >
                    {l.resume}
                  </button>
                ) : null}
              </div>
            ) : onCancel && subscription.entitled && current && !isFreePlan(current) ? (
              <div>
                <button
                  type="button"
                  disabled={busy}
                  onClick={onCancel}
                  className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline disabled:opacity-50"
                >
                  {l.cancel}
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>

      <section id="lungor-plans" aria-label={l.plansHeading} className="scroll-mt-6">
        <PricingTable
          plans={plans}
          authenticated
          currentPlanCode={subscription?.planCode}
          busy={busy}
          locale={locale}
          formatUnit={formatUnit}
          heading={l.plansHeading}
          labels={pricingLabels}
          onSelect={select}
        />
      </section>
    </div>
  );
}
