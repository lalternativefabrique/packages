import { useEffect, useMemo, useState } from 'react';
import { APPLE_PAY, canUseApplePay, METHOD_TIMING, type CheckoutMethod } from './methods.js';

export interface CheckoutMethodPickerProps {
  /** Methods from `GET /finance/checkout/methods`, in the order Lungor returned them. */
  methods: CheckoutMethod[];
  /** Called with the chosen method id, to be sent as `payment_method` on checkout. */
  onSelect: (methodId: string) => void;
  /** Disables every control while the caller opens the session. */
  busy?: boolean;
  /** What the payer is about to pay, already formatted (e.g. "19,00 €"). */
  amountLabel?: string;
  heading?: string;
  submitLabel?: string;
  className?: string;
}

const ICONS: Record<string, string> = {
  card: '💳',
  apple_pay: '',
  paypal: '🅿️',
  bank_transfer: '🏦',
};

/**
 * Asks the payer how they want to pay, then hands the answer back.
 *
 * Renders exactly what it is given: the server decides which methods a plan
 * accepts, so nothing offered here can be refused a step later. Apple Pay is
 * the one exception, dropped when the browser cannot honour it.
 *
 * Styling is Tailwind on the shadcn design tokens (`bg-background`, `ring`,
 * `border-input`…) without importing any component: those tokens exist in every
 * app, the components do not, and each app's copy has drifted.
 */
export function CheckoutMethodPicker({
  methods,
  onSelect,
  busy = false,
  amountLabel,
  heading = 'Comment souhaitez-vous payer ?',
  submitLabel = 'Continuer',
  className = '',
}: CheckoutMethodPickerProps) {
  const [applePayReady, setApplePayReady] = useState(false);

  // After mount only: the server renders the same markup for everyone, and
  // Apple Pay depends on the browser it lands in.
  useEffect(() => setApplePayReady(canUseApplePay()), []);

  const applePay = useMemo(
    () => (applePayReady ? methods.find((m) => m.id === APPLE_PAY) : undefined),
    [methods, applePayReady],
  );
  const listed = useMemo(() => methods.filter((m) => m.id !== APPLE_PAY), [methods]);

  const [selected, setSelected] = useState<string | undefined>(listed[0]?.id);
  useEffect(() => {
    setSelected((current) =>
      current && listed.some((m) => m.id === current) ? current : listed[0]?.id,
    );
  }, [listed]);

  if (methods.length === 0) {
    return (
      <p className={`text-sm text-muted-foreground ${className}`.trim()}>
        Aucun moyen de paiement n’est disponible pour cette offre.
      </p>
    );
  }

  return (
    <div className={`flex flex-col gap-4 ${className}`.trim()}>
      <div>
        <h2 className="text-base font-medium text-foreground">{heading}</h2>
        {amountLabel ? (
          <p className="mt-1 text-sm text-muted-foreground">{amountLabel}</p>
        ) : null}
      </div>

      {applePay ? (
        <>
          <button
            type="button"
            disabled={busy}
            onClick={() => onSelect(applePay.id)}
            className="flex h-12 w-full items-center justify-center gap-1.5 rounded-md bg-black text-[15px] font-medium text-white transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
          >
            <span aria-hidden="true"></span> Pay
          </button>
          {listed.length > 0 ? (
            <div className="flex items-center gap-3" aria-hidden="true">
              <span className="h-px flex-1 bg-border" />
              <span className="text-xs text-muted-foreground">ou</span>
              <span className="h-px flex-1 bg-border" />
            </div>
          ) : null}
        </>
      ) : null}

      {listed.length > 0 ? (
        <>
          <div role="radiogroup" aria-label={heading} className="flex flex-col gap-2">
            {listed.map((method) => {
              const isSelected = method.id === selected;
              return (
                <label
                  key={method.id}
                  className={`flex cursor-pointer items-center gap-3 rounded-md border p-4 transition-colors ${
                    isSelected
                      ? 'border-primary bg-accent/50 ring-1 ring-primary'
                      : 'border-input hover:bg-accent/30'
                  } ${busy ? 'pointer-events-none opacity-50' : ''}`}
                >
                  <input
                    type="radio"
                    name="lungor-checkout-method"
                    value={method.id}
                    checked={isSelected}
                    disabled={busy}
                    onChange={() => setSelected(method.id)}
                    className="size-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                  <span aria-hidden="true" className="text-lg leading-none">
                    {ICONS[method.id] ?? '💳'}
                  </span>
                  <span className="flex min-w-0 flex-col">
                    <span className="text-sm font-medium text-foreground">{method.label}</span>
                    {METHOD_TIMING[method.id] ? (
                      <span className="text-xs text-muted-foreground">
                        {METHOD_TIMING[method.id]}
                      </span>
                    ) : null}
                  </span>
                </label>
              );
            })}
          </div>

          <button
            type="button"
            disabled={busy || !selected}
            onClick={() => selected && onSelect(selected)}
            className="h-10 w-full rounded-md bg-primary text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50"
          >
            {busy ? 'Redirection…' : submitLabel}
          </button>
        </>
      ) : null}
    </div>
  );
}
