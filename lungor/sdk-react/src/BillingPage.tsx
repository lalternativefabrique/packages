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
import type { BillingAction, BillingActions, BillingCard } from './useBillingActions.js';

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
  paymentMethod?: string;
  /** `{month}` and `{year}` are substituted. */
  cardExpires?: string;
  replaceCard?: string;
  /** `{plan}` is substituted. */
  quoteTitle?: string;
  /** `{now}` and `{recurring}` are substituted. */
  quoteBody?: string;
  /** `{card}` is substituted. */
  quoteCard?: string;
  quoteSavedCard?: string;
  confirm?: string;
  dismiss?: string;
  cancelTitle?: string;
  /** `{date}` is substituted. */
  cancelBody?: string;
  keepSubscription?: string;
  /** Shown when the subscription is past due and no card can be read. */
  updatePaymentMethod?: string;
  /** Shown when an action failed for a reason worth retrying. */
  failures?: Partial<Record<BillingAction, string>>;
  /** Shown when Lungor refused the request itself. */
  refusals?: Partial<Record<BillingAction, string>>;
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
  paymentMethod: 'Moyen de paiement',
  cardExpires: 'expire {month}/{year}',
  replaceCard: 'Changer de carte',
  quoteTitle: 'Passer à {plan}',
  quoteBody: '{now} à payer maintenant pour les jours restants de votre période, puis {recurring}.',
  quoteCard: 'Prélevés sur votre carte {card}, sans nouvelle saisie.',
  quoteSavedCard: 'Prélevés sur votre carte enregistrée, sans nouvelle saisie.',
  confirm: 'Confirmer',
  dismiss: 'Annuler',
  cancelTitle: 'Résilier votre abonnement ?',
  cancelBody: 'Vous gardez votre offre jusqu’au {date}, puis plus rien n’est prélevé. Vous pouvez revenir sur votre décision d’ici là.',
  keepSubscription: 'Garder mon abonnement',
  updatePaymentMethod: 'Mettre à jour le moyen de paiement',
  refusals: {
    change: 'Le paiement a été refusé. Vérifiez votre carte ou changez-en, puis réessayez.',
    cancel: 'Cet abonnement ne peut plus être résilié.',
    resume: 'La période est terminée : choisissez une offre pour vous réabonner.',
    withdraw: 'Ce changement ne peut plus être annulé.',
    card: 'Le changement de carte n’est pas disponible pour cet abonnement.',
  },
  failures: {
    change: 'Le changement d’offre a échoué. Réessayez dans un instant.',
    cancel: 'La résiliation a échoué. Réessayez dans un instant.',
    resume: 'La reprise a échoué. Réessayez dans un instant.',
    withdraw: 'L’annulation du changement a échoué. Réessayez dans un instant.',
    card: 'Le changement de carte n’a pas pu être ouvert. Réessayez dans un instant.',
  },
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
  /**
   * From useBillingActions. Given, the page changes plan with a quote naming
   * the card, confirms a cancellation, resumes, and shows the card on file
   * with a way to replace it; the onChangePlan/onCancel/onResume/
   * onWithdrawPendingPlan props are then ignored.
   */
  actions?: BillingActions;
  className?: string;
}

/** The same labels in English, for an app that is not French. */
export const BILLING_PAGE_LABELS_EN: Required<BillingPageLabels> = {
  heading: 'Subscription',
  description: 'Your plan, its renewal and the plans available.',
  currentPlan: 'Your current plan',
  noPlan: 'No active plan',
  noPlanDetail: 'Pick a plan below to get started.',
  renewsOn: 'Renews on {date}',
  endsOn: 'Access until {date}',
  pendingChange: 'Moves to {plan} on {date}',
  withdrawPending: 'Cancel this change',
  cancelScheduled: 'Cancellation scheduled: no renewal will follow.',
  resume: 'Resume subscription',
  cancel: 'Cancel',
  pastDue: 'The last payment failed. Update your payment method to keep your plan.',
  remaining: 'Remaining',
  plansHeading: 'Change plan',
  paymentMethod: 'Payment method',
  cardExpires: 'expires {month}/{year}',
  replaceCard: 'Change card',
  quoteTitle: 'Move to {plan}',
  quoteBody: '{now} to pay now for the rest of your period, then {recurring}.',
  quoteCard: 'Charged to your card {card}, nothing to enter.',
  quoteSavedCard: 'Charged to your saved card, nothing to enter.',
  confirm: 'Confirm',
  dismiss: 'Cancel',
  cancelTitle: 'Cancel your subscription?',
  cancelBody: 'You keep your plan until {date}, then nothing more is charged. You can change your mind until then.',
  keepSubscription: 'Keep my subscription',
  updatePaymentMethod: 'Update payment method',
  refusals: {
    change: 'The payment was declined. Check your card or change it, then try again.',
    cancel: 'This subscription can no longer be cancelled.',
    resume: 'The period has ended: pick a plan to subscribe again.',
    withdraw: 'This change can no longer be withdrawn.',
    card: 'Changing the card is not available for this subscription.',
  },
  failures: {
    change: 'The plan change failed. Try again in a moment.',
    cancel: 'The cancellation failed. Try again in a moment.',
    resume: 'Resuming failed. Try again in a moment.',
    withdraw: 'Withdrawing the change failed. Try again in a moment.',
    card: 'The card page could not be opened. Try again in a moment.',
  },
};

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

export function cardLabel(card: BillingCard): string {
  const brand = card.brand ? card.brand.charAt(0).toUpperCase() + card.brand.slice(1) : 'Carte';
  return `${brand} •••• ${card.last4}`;
}

function cardExpiry(card: BillingCard, template: string): string {
  if (!card.expiresAt) return '';
  const d = new Date(card.expiresAt);
  if (Number.isNaN(d.getTime())) return '';
  return fill(template, {
    month: String(d.getUTCMonth() + 1).padStart(2, '0'),
    year: String(d.getUTCFullYear()).slice(2),
  });
}

function Dialog({
  title,
  children,
  onDismiss,
  actions,
}: {
  title: string;
  children: ReactNode;
  onDismiss: () => void;
  actions: ReactNode;
}) {
  return (
    <div
      role="presentation"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={onDismiss}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="w-full max-w-md rounded-xl border border-border bg-card p-6 text-card-foreground shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold text-foreground">{title}</h2>
        <div className="mt-2 text-sm text-muted-foreground">{children}</div>
        <div className="mt-6 flex flex-wrap justify-end gap-2">{actions}</div>
      </div>
    </div>
  );
}

const secondaryButton =
  'rounded-md border border-border px-4 py-2 text-sm font-medium text-foreground hover:bg-muted disabled:opacity-50';
const primaryButton =
  'rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50';

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
  actions,
  className = '',
}: BillingPageProps) {
  const l = {
    ...DEFAULT_LABELS,
    ...labels,
    failures: { ...DEFAULT_LABELS.failures, ...labels?.failures },
    refusals: { ...DEFAULT_LABELS.refusals, ...labels?.refusals },
  };
  if (actions) {
    onChangePlan = actions.changePlan;
    onCancel = actions.askCancel;
    onResume = actions.resume;
    onWithdrawPendingPlan = actions.withdrawPending;
    busy = busy || actions.busy;
  }
  const hasSubscription = Boolean(
    subscription && subscription.status !== 'no_subscription' && subscription.planCode,
  );
  const current = useMemo(
    () => plans.find((p) => p.code === subscription?.planCode),
    [plans, subscription?.planCode],
  );
  // Only a paid subscription moves by change-plan: a free plan holds no card
  // to charge, so leaving it is a checkout.
  const paying = Boolean(
    hasSubscription && subscription?.entitled && current && !isFreePlan(current),
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
    if (paying && onChangePlan) onChangePlan(plan);
    else onCheckout(plan);
  }, [requested, plans, subscription, onCheckout, onChangePlan, paying]);

  const select = (plan: PricingPlan, intent: PricingIntent) => {
    if (intent !== 'checkout') return;
    if ((actions ? paying : hasSubscription && subscription?.entitled) && onChangePlan) {
      onChangePlan(plan);
    } else onCheckout(plan);
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

            {actions && (actions.card || pastDue) ? (
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4 text-sm">
                <div>
                  <p className="text-muted-foreground">{l.paymentMethod}</p>
                  {actions.card ? (
                    <p className="font-medium text-foreground">
                      {cardLabel(actions.card)}
                      {actions.card.expiresAt ? ` · ${cardExpiry(actions.card, l.cardExpires)}` : ''}
                    </p>
                  ) : null}
                </div>
                <button
                  type="button"
                  disabled={busy}
                  onClick={actions.replaceCard}
                  className={secondaryButton}
                >
                  {actions.card ? l.replaceCard : l.updatePaymentMethod}
                </button>
              </div>
            ) : null}

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

      {actions?.failure ? (
        <p role="alert" className="text-sm text-destructive">
          {actions.failure.reason === 'unavailable'
            ? l.failures[actions.failure.action]
            : l.refusals[actions.failure.action]}
        </p>
      ) : null}

      {actions?.quote ? (
        <Dialog
          title={fill(l.quoteTitle, { plan: actions.quote.plan.name })}
          onDismiss={actions.dismissQuote}
          actions={
            <>
              <button type="button" className={secondaryButton} onClick={actions.dismissQuote}>
                {l.dismiss}
              </button>
              <button
                type="button"
                className={primaryButton}
                disabled={busy}
                onClick={actions.confirmPlanChange}
              >
                {l.confirm}
              </button>
            </>
          }
        >
          <p>
            {fill(l.quoteBody, {
              now: formatPrice(actions.quote.nowCents, actions.quote.plan.currency, locale),
              recurring:
                formatPrice(actions.quote.recurringCents, actions.quote.plan.currency, locale) +
                intervalSuffix(actions.quote.plan),
            })}
          </p>
          <p className="mt-2">
            {actions.quote.card
              ? fill(l.quoteCard, { card: cardLabel(actions.quote.card) })
              : l.quoteSavedCard}
          </p>
        </Dialog>
      ) : null}

      {actions?.confirmingCancel ? (
        <Dialog
          title={l.cancelTitle}
          onDismiss={actions.dismissCancel}
          actions={
            <>
              <button type="button" className={secondaryButton} onClick={actions.dismissCancel}>
                {l.keepSubscription}
              </button>
              <button
                type="button"
                className="rounded-md bg-destructive px-4 py-2 text-sm font-medium text-white hover:bg-destructive/90 disabled:opacity-50"
                disabled={busy}
                onClick={actions.confirmCancel}
              >
                {l.cancel}
              </button>
            </>
          }
        >
          {fill(l.cancelBody, { date: periodEnd })}
        </Dialog>
      ) : null}

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
