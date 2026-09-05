/**
 * One metered unit's allowance on a plan, per billing period.
 *
 * A ceiling, not a wallet: nothing is credited when the period opens and
 * nothing carries over when it closes.
 */
export interface PricingAllocation {
  /** The metered unit, in the app's own vocabulary (`credit`, `synthesis`, `email`). */
  unit: string;
  amount: number;
}

/**
 * A plan as Lungor's `GET /finance/plans` returns it, plus what only the
 * calling app knows.
 *
 * The wire fields are named as Lungor names them so a row can be handed
 * straight through from the read: restating a price on the way is how a page
 * ends up showing one figure while checkout charges another.
 */
export interface PricingPlan {
  /** The value a checkout is opened against. */
  id: string;
  code: string;
  name: string;
  /** Minor units of `currency` (2900 = 29.00 EUR). Never a float — a cent lost to rounding fails an audit. */
  amount: number;
  /** ISO 4217, e.g. `EUR`. */
  currency: string;
  /** Billing cadence, e.g. `month`. Omit on a plan billed once. */
  interval?: string;
  /** How many `interval`s a period spans. Defaults to 1. */
  intervalCount?: number;
  /** What the plan includes per period, per unit. Empty when it caps nothing. */
  allocations?: PricingAllocation[];
  /**
   * Whether this plan can be bought right now.
   *
   * Defaults to true. Set it false for a tier withheld from sale, so the card
   * stops short of a button that only leads to a refusal.
   */
  purchasable?: boolean;
  /** Selling points, rendered under the allowance. */
  features?: string[];
  /** Singles this plan out as the recommended one. At most one should carry it. */
  highlighted?: boolean;
  /** Replaces the computed price with free text, for a tier quoted on request. */
  priceLabel?: string;
}

/**
 * Whether the plan charges nothing.
 *
 * A free tier cannot go through checkout at all: a payment session needs a PSP
 * credential that a plan charging nothing has none of, so the only way onto one
 * is a grant made after sign-up.
 */
export function isFreePlan(plan: PricingPlan): boolean {
  return plan.amount <= 0;
}

const MINOR_UNITS = 100;

/**
 * Formats a plan's price in the visitor's locale.
 *
 * Divides by 100 at the very end, once, so the integer the ledger holds is the
 * integer that is rendered.
 */
export function formatPrice(amount: number, currency: string, locale?: string): string {
  try {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency,
      minimumFractionDigits: amount % MINOR_UNITS === 0 ? 0 : 2,
    }).format(amount / MINOR_UNITS);
  } catch {
    return `${(amount / MINOR_UNITS).toFixed(2)} ${currency}`;
  }
}
