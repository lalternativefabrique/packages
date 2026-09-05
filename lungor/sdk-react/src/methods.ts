/**
 * A payment method as Lungor's `GET /finance/checkout/methods` returns it.
 *
 * `id` is Lungor's own vocabulary, never a provider's spelling, and travels
 * back verbatim as `payment_method` on `POST /finance/checkout`.
 */
export interface CheckoutMethod {
  id: string;
  label: string;
}

/**
 * What the payer waits for once they have paid. Shown before the choice, not
 * after: a transfer that settles in two days is a fine choice to make knowingly
 * and a bad one to discover on the provider's page.
 */
export const METHOD_TIMING: Record<string, string> = {
  card: 'Paiement immédiat',
  apple_pay: 'Paiement immédiat',
  paypal: 'Paiement immédiat',
  bank_transfer: 'Sous 2 jours ouvrés',
};

export const APPLE_PAY = 'apple_pay';

/**
 * Whether this browser can actually pay with Apple Pay — Safari or iOS, with a
 * card already set up. Offering it anywhere else sends the payer to a provider
 * page that refuses them.
 */
export function canUseApplePay(): boolean {
  if (typeof window === 'undefined') return false;
  const session = (window as { ApplePaySession?: { canMakePayments?: () => boolean } })
    .ApplePaySession;
  try {
    return session?.canMakePayments?.() === true;
  } catch {
    return false;
  }
}
