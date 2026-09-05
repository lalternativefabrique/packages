import { useMemo } from 'react';
import { formatPrice, isFreePlan, type PricingAllocation, type PricingPlan } from './plans.js';

/**
 * What a plan's button does when pressed, so the caller routes rather than
 * guesses.
 *
 * `signup` covers both a visitor with no account and a free tier, which cannot
 * be bought: a plan charging nothing has no PSP credential to open a payment
 * session with, so it is granted after sign-up instead.
 */
export type PricingIntent = 'checkout' | 'signup';

export interface PricingTableLabels {
  freeCta?: string;
  purchaseCta?: string;
  currentCta?: string;
  unavailable?: string;
  /** Marks the recommended plan. */
  highlight?: string;
  /** Column header over the plan names in `table` layout. */
  planColumn?: string;
  /** Row label for the price in `table` layout. */
  priceRow?: string;
  /** Row label for the allowances in `table` layout. */
  includedRow?: string;
  empty?: string;
  /** Describes the table to a screen reader in `table` layout. */
  tableCaption?: string;
}

export interface PricingTableProps {
  /** Plans from `GET /finance/plans`, in the order Lungor returned them (by rank). */
  plans: PricingPlan[];
  /**
   * Called with the plan and what pressing its button should do.
   *
   * The component never opens a checkout itself: the app key that authorises
   * one is a server-side secret, so the caller's own backend does it.
   */
  onSelect: (plan: PricingPlan, intent: PricingIntent) => void;
  /**
   * Whether a visitor is signed in. Defaults to true.
   *
   * When false every button routes to `signup`, since a public pricing page
   * that sent a stranger into checkout would only bounce them off a login wall
   * with their intent lost.
   */
  authenticated?: boolean;
  /** Code of the plan the visitor already holds, shown as current and not offered again. */
  currentPlanCode?: string;
  /** Disables every control while the caller opens a session. */
  busy?: boolean;
  /**
   * `cards` reads at any plan count; `table` compares row by row, which is
   * clearer for two or three plans and cramped beyond that.
   */
  layout?: 'cards' | 'table';
  /** How the price is formatted. Defaults to the visitor's browser locale. */
  locale?: string;
  /** Renders a unit as a shopper reads it (`credit` → "crédits"). */
  formatUnit?: (allocation: PricingAllocation) => string;
  heading?: string;
  description?: string;
  labels?: PricingTableLabels;
  className?: string;
}

const DEFAULT_LABELS: Required<PricingTableLabels> = {
  freeCta: 'Commencer gratuitement',
  purchaseCta: 'Choisir cette offre',
  currentCta: 'Votre offre actuelle',
  unavailable: 'Bientôt disponible',
  highlight: 'Recommandé',
  planColumn: 'Offre',
  priceRow: 'Prix',
  includedRow: 'Inclus',
  empty: 'Aucune offre n’est disponible pour le moment.',
  tableCaption: 'Comparatif des offres',
};

const INTERVAL_LABELS: Record<string, string> = {
  day: 'jour',
  week: 'semaine',
  month: 'mois',
  year: 'an',
};

function intervalSuffix(plan: PricingPlan): string {
  if (!plan.interval) return '';
  const count = plan.intervalCount ?? 1;
  const unit = INTERVAL_LABELS[plan.interval] ?? plan.interval;
  return count > 1 ? ` / ${count} ${unit}` : ` / ${unit}`;
}

function defaultFormatUnit(allocation: PricingAllocation): string {
  return `${allocation.amount.toLocaleString()} ${allocation.unit}`;
}

interface PlanState {
  plan: PricingPlan;
  price: string;
  suffix: string;
  allocations: string[];
  isCurrent: boolean;
  isAvailable: boolean;
  intent: PricingIntent;
  cta: string;
}

const BUTTON_BASE =
  'inline-flex h-11 w-full items-center justify-center rounded-md px-4 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50';

/**
 * Renders an app's catalogue and hands back the plan a shopper picked.
 *
 * Every figure comes from the rows it is given, which are Lungor's own: a page
 * that restated a price in its markup would eventually disagree with the
 * checkout that charges it.
 *
 * Styling is Tailwind on the shadcn design tokens (`bg-background`, `ring`,
 * `border-input`…) without importing any component: those tokens exist in every
 * app, the components do not, and each app's copy has drifted.
 */
export function PricingTable({
  plans,
  onSelect,
  authenticated = true,
  currentPlanCode,
  busy = false,
  layout = 'cards',
  locale,
  formatUnit = defaultFormatUnit,
  heading,
  description,
  labels,
  className = '',
}: PricingTableProps) {
  const text = { ...DEFAULT_LABELS, ...labels };

  const states = useMemo<PlanState[]>(
    () =>
      plans.map((plan) => {
        const free = isFreePlan(plan);
        const isCurrent = currentPlanCode !== undefined && plan.code === currentPlanCode;
        const isAvailable = (plan.purchasable ?? true) && !isCurrent;
        const intent: PricingIntent = free || !authenticated ? 'signup' : 'checkout';
        return {
          plan,
          price: plan.priceLabel ?? formatPrice(plan.amount, plan.currency, locale),
          suffix: plan.priceLabel ? '' : intervalSuffix(plan),
          allocations: (plan.allocations ?? []).map(formatUnit),
          isCurrent,
          isAvailable,
          intent,
          cta: isCurrent ? text.currentCta : free ? text.freeCta : text.purchaseCta,
        };
      }),
    [plans, authenticated, currentPlanCode, locale, formatUnit, text.currentCta, text.freeCta, text.purchaseCta],
  );

  if (states.length === 0) {
    return <p className={`text-sm text-muted-foreground ${className}`.trim()}>{text.empty}</p>;
  }

  const header =
    heading || description ? (
      <div className="flex flex-col gap-1">
        {heading ? <h2 className="text-xl font-semibold text-foreground">{heading}</h2> : null}
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
    ) : null;

  return (
    <section className={`flex flex-col gap-6 ${className}`.trim()} aria-label={heading || undefined}>
      {header}
      {layout === 'table' ? (
        <ComparisonTable states={states} text={text} busy={busy} onSelect={onSelect} />
      ) : (
        <PlanCards states={states} text={text} busy={busy} onSelect={onSelect} />
      )}
    </section>
  );
}

interface LayoutProps {
  states: PlanState[];
  text: Required<PricingTableLabels>;
  busy: boolean;
  onSelect: (plan: PricingPlan, intent: PricingIntent) => void;
}

function PlanAction({
  state,
  busy,
  onSelect,
  text,
}: {
  state: PlanState;
  busy: boolean;
  onSelect: LayoutProps['onSelect'];
  text: Required<PricingTableLabels>;
}) {
  if (!state.isAvailable) {
    return (
      <p className="flex h-11 items-center justify-center rounded-md border border-dashed border-input px-4 text-sm text-muted-foreground">
        {state.isCurrent ? text.currentCta : text.unavailable}
      </p>
    );
  }
  return (
    <button
      type="button"
      disabled={busy}
      onClick={() => onSelect(state.plan, state.intent)}
      className={`${BUTTON_BASE} ${
        state.plan.highlighted
          ? 'bg-primary text-primary-foreground hover:bg-primary/90'
          : 'border border-input bg-background text-foreground hover:bg-accent hover:text-accent-foreground'
      }`}
    >
      {state.cta}
      <span className="sr-only"> — {state.plan.name}</span>
    </button>
  );
}

function Price({ state }: { state: PlanState }) {
  return (
    <p className="flex items-baseline gap-1">
      <span className="text-3xl font-semibold tracking-tight text-foreground">{state.price}</span>
      {state.suffix ? (
        <span className="text-sm text-muted-foreground">{state.suffix}</span>
      ) : null}
    </p>
  );
}

function PlanCards({ states, text, busy, onSelect }: LayoutProps) {
  return (
    <ul
      className="grid list-none gap-4 p-0 sm:grid-cols-2 lg:grid-cols-[repeat(auto-fit,minmax(15rem,1fr))]"
      role="list"
    >
      {states.map((state) => (
        <li
          key={state.plan.code}
          className={`flex flex-col gap-4 rounded-lg border bg-background p-6 ${
            state.plan.highlighted ? 'border-primary ring-1 ring-primary' : 'border-input'
          }`}
        >
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between gap-2">
              <h3 className="text-base font-medium text-foreground">{state.plan.name}</h3>
              {state.plan.highlighted ? (
                <span className="rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                  {text.highlight}
                </span>
              ) : null}
            </div>
            <Price state={state} />
          </div>

          {state.allocations.length > 0 || (state.plan.features ?? []).length > 0 ? (
            <ul className="flex flex-1 flex-col gap-2 text-sm text-muted-foreground">
              {state.allocations.map((line) => (
                <li key={line} className="font-medium text-foreground">
                  {line}
                </li>
              ))}
              {(state.plan.features ?? []).map((feature) => (
                <li key={feature}>{feature}</li>
              ))}
            </ul>
          ) : (
            <div className="flex-1" />
          )}

          <PlanAction state={state} busy={busy} onSelect={onSelect} text={text} />
        </li>
      ))}
    </ul>
  );
}

function ComparisonTable({ states, text, busy, onSelect }: LayoutProps) {
  const hasAllocations = states.some((s) => s.allocations.length > 0);
  const features = Array.from(
    new Set(states.flatMap((s) => s.plan.features ?? [])),
  );

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-left text-sm">
        <caption className="sr-only">{text.tableCaption}</caption>
        <thead>
          <tr>
            <th scope="col" className="w-1/3 p-4 font-medium text-muted-foreground">
              {text.planColumn}
            </th>
            {states.map((state) => (
              <th
                key={state.plan.code}
                scope="col"
                className={`border-b p-4 align-bottom ${
                  state.plan.highlighted ? 'border-primary' : 'border-input'
                }`}
              >
                <span className="flex flex-col gap-1">
                  <span className="text-base font-medium text-foreground">{state.plan.name}</span>
                  {state.plan.highlighted ? (
                    <span className="w-fit rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                      {text.highlight}
                    </span>
                  ) : null}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            <th scope="row" className="border-b border-input p-4 font-medium text-muted-foreground">
              {text.priceRow}
            </th>
            {states.map((state) => (
              <td key={state.plan.code} className="border-b border-input p-4">
                <Price state={state} />
              </td>
            ))}
          </tr>

          {hasAllocations ? (
            <tr>
              <th
                scope="row"
                className="border-b border-input p-4 font-medium text-muted-foreground"
              >
                {text.includedRow}
              </th>
              {states.map((state) => (
                <td key={state.plan.code} className="border-b border-input p-4 text-foreground">
                  {state.allocations.length > 0 ? (
                    <ul className="flex flex-col gap-1">
                      {state.allocations.map((line) => (
                        <li key={line}>{line}</li>
                      ))}
                    </ul>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
              ))}
            </tr>
          ) : null}

          {features.map((feature) => (
            <tr key={feature}>
              <th
                scope="row"
                className="border-b border-input p-4 font-normal text-muted-foreground"
              >
                {feature}
              </th>
              {states.map((state) => {
                const included = (state.plan.features ?? []).includes(feature);
                return (
                  <td key={state.plan.code} className="border-b border-input p-4">
                    <span aria-hidden="true" className={included ? 'text-foreground' : 'text-muted-foreground'}>
                      {included ? '✓' : '—'}
                    </span>
                    <span className="sr-only">
                      {included ? 'Inclus' : 'Non inclus'} — {state.plan.name}
                    </span>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr>
            <td />
            {states.map((state) => (
              <td key={state.plan.code} className="p-4 align-top">
                <PlanAction state={state} busy={busy} onSelect={onSelect} text={text} />
              </td>
            ))}
          </tr>
        </tfoot>
      </table>
    </div>
  );
}
