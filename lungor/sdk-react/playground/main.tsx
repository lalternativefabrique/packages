import { useState } from "react"
import { createRoot } from "react-dom/client"
import { CheckoutMethodPicker } from "../src/CheckoutMethodPicker.js"
import { CheckoutOutcome } from "../src/CheckoutOutcome.js"
import { BillingPage, type BillingSubscription } from "../src/BillingPage.js"
import { BUSINESS_BUYERS_NOT_ACCEPTED, type CheckoutSession, type CheckoutStatus } from "../src/checkout.js"
import { PricingTable } from "../src/PricingTable.js"
import { formatPrice, type PricingAllocation, type PricingPlan } from "../src/plans.js"
import { METHODS, PLANS, STAFF_PLAN, UNIT_LABELS } from "./catalogue.js"
import "./styles.css"

function formatUnit(allocation: PricingAllocation): string {
  const forms = UNIT_LABELS[allocation.unit]
  if (!forms) return `${allocation.amount.toLocaleString("fr-FR")} ${allocation.unit}`
  const [one, many] = forms
  return `${allocation.amount.toLocaleString("fr-FR")} ${allocation.amount > 1 ? many : one}`
}

type Screen = "pricing" | "methods" | "outcome" | "billing"

type SubState = "none" | "active" | "pending" | "canceling" | "past_due"

function fakeSubscription(state: SubState): BillingSubscription | undefined {
  const end = new Date(Date.now() + 12 * 24 * 3600 * 1000).toISOString()
  switch (state) {
    case "none":
      return { entitled: false, status: "no_subscription" }
    case "active":
      return { entitled: true, status: "active", planCode: "pro", currentPeriodEnd: end, balances: { synthesis: 212 } }
    case "pending":
      return { entitled: true, status: "active", planCode: "pro", currentPeriodEnd: end, pendingPlanCode: "solo", pendingPlanEffectiveAt: end }
    case "canceling":
      return { entitled: true, status: "canceled", planCode: "pro", currentPeriodEnd: end, cancelAtPeriodEnd: true }
    case "past_due":
      return { entitled: true, status: "past_due", planCode: "pro", currentPeriodEnd: end }
  }
}

const OUTCOMES: CheckoutStatus[] = ["completed", "failed", "canceled", "expired", "redirected"]

// Stands in for the app's proxy: answers "redirected" twice, then the chosen
// ending, so the polling is visible.
function fakeFetchSession(ending: CheckoutStatus, reason: string) {
  let calls = 0
  return async (sessionId: string): Promise<CheckoutSession> => {
    await new Promise((r) => setTimeout(r, 400))
    calls += 1
    const status = calls < 3 ? "redirected" : ending
    return {
      sessionId,
      status,
      paid: status === "completed",
      failureReason: status === "failed" ? reason : undefined,
    }
  }
}

function App() {
  const [screen, setScreen] = useState<Screen>("pricing")
  const [layout, setLayout] = useState<"cards" | "table">("cards")
  const [authenticated, setAuthenticated] = useState(true)
  const [busy, setBusy] = useState(false)
  const [empty, setEmpty] = useState(false)
  const [leakStaff, setLeakStaff] = useState(false)
  const [current, setCurrent] = useState<string | undefined>(undefined)
  const [log, setLog] = useState<string[]>([])
  const [ending, setEnding] = useState<CheckoutStatus>("failed")
  const [reason, setReason] = useState("insufficient_funds")
  const [run, setRun] = useState(0)
  const [hero, setHero] = useState(true)
  const [subState, setSubState] = useState<SubState>("active")
  const [refuseBusiness, setRefuseBusiness] = useState(false)

  const record = (line: string) => setLog((l) => [line, ...l].slice(0, 6))

  const plans = empty ? [] : leakStaff ? [...PLANS, STAFF_PLAN] : PLANS

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-2 px-6 py-4">
          <h1 className="mr-4 text-sm font-semibold">lungor-sdk-react</h1>
          <Tab active={screen === "pricing"} onClick={() => setScreen("pricing")}>
            PricingTable
          </Tab>
          <Tab active={screen === "methods"} onClick={() => setScreen("methods")}>
            CheckoutMethodPicker
          </Tab>
          <Tab active={screen === "outcome"} onClick={() => setScreen("outcome")}>
            CheckoutOutcome
          </Tab>
          <Tab active={screen === "billing"} onClick={() => setScreen("billing")}>
            BillingPage
          </Tab>
        </div>
      </header>

      <div className="mx-auto flex max-w-5xl flex-wrap gap-4 px-6 py-4 text-sm">
        {screen === "pricing" && (
          <>
            <Toggle checked={layout === "table"} onChange={(v) => setLayout(v ? "table" : "cards")}>
              Comparatif
            </Toggle>
            <Toggle checked={authenticated} onChange={setAuthenticated}>
              Connecté
            </Toggle>
            <Toggle checked={current === "solo"} onChange={(v) => setCurrent(v ? "solo" : undefined)}>
              Abonné Solo
            </Toggle>
            <Toggle checked={empty} onChange={setEmpty}>
              Catalogue vide
            </Toggle>
            <Toggle checked={leakStaff} onChange={setLeakStaff}>
              Fuite d’un plan staff
            </Toggle>
          </>
        )}
        {screen === "outcome" && (
          <>
            <label className="inline-flex items-center gap-2">
              Fin
              <select
                value={ending}
                onChange={(e) => setEnding(e.target.value as CheckoutStatus)}
                className="rounded-md border border-input bg-background px-2 py-1"
              >
                {OUTCOMES.map((o) => (
                  <option key={o} value={o}>
                    {o === "redirected" ? "jamais (timeout)" : o}
                  </option>
                ))}
              </select>
            </label>
            <label className="inline-flex items-center gap-2">
              Raison
              <input
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                className="rounded-md border border-input bg-background px-2 py-1 font-mono text-xs"
              />
            </label>
            <Toggle checked={hero} onChange={setHero}>
              Variante hero
            </Toggle>
            <button
              onClick={() => setRun((n) => n + 1)}
              className="rounded-md border border-input px-3 py-1 hover:bg-muted"
            >
              Rejouer le retour
            </button>
          </>
        )}
        {screen === "billing" && (
          <label className="inline-flex items-center gap-2">
            Souscription
            <select
              value={subState}
              onChange={(e) => setSubState(e.target.value as SubState)}
              className="rounded-md border border-input bg-background px-2 py-1"
            >
              {(["none", "active", "pending", "canceling", "past_due"] as SubState[]).map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        )}
        {screen === "methods" && (
          <Toggle checked={refuseBusiness} onChange={setRefuseBusiness}>
            Offre réservée aux particuliers
          </Toggle>
        )}
        {screen !== "outcome" && (
          <Toggle checked={busy} onChange={setBusy}>
            Occupé
          </Toggle>
        )}
      </div>

      {leakStaff && screen === "pricing" && (
        <div className="mx-auto max-w-5xl px-6 pb-2">
          <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">
            Le plan « Staff » ci-dessous ne doit jamais atteindre une page tarifaire. Lungor le
            retient à la source ; s’il s’affiche ici, la route catalogue l’a laissé passer.
          </p>
        </div>
      )}

      <main className="mx-auto max-w-5xl px-6 py-6">
        {screen === "pricing" ? (
          <PricingTable
            plans={plans}
            layout={layout}
            authenticated={authenticated}
            currentPlanCode={current}
            busy={busy}
            locale="fr-FR"
            formatUnit={formatUnit}
            heading="Nos offres"
            description="Une source traitée compte pour une synthèse."
            onSelect={(plan: PricingPlan, intent) =>
              record(`${intent} → ${plan.code} (${formatPrice(plan.amount, plan.currency, "fr-FR")})`)
            }
          />
        ) : screen === "methods" ? (
          <CheckoutMethodPicker
            methods={METHODS}
            busy={busy}
            amountLabel={formatPrice(2900, "EUR", "fr-FR")}
            refusal={refuseBusiness ? BUSINESS_BUYERS_NOT_ACCEPTED : undefined}
            onSelect={(id, { buyerKind }) => record(`payment_method → ${id}, buyer_kind → ${buyerKind}`)}
          />
        ) : screen === "billing" ? (
          <BillingPage
            plans={PLANS}
            subscription={fakeSubscription(subState)}
            busy={busy}
            locale="fr-FR"
            formatUnit={formatUnit}
            fetchSession={fakeFetchSession(ending, reason)}
            onCheckout={(plan) => record(`onCheckout → ${plan.code}`)}
            onChangePlan={(plan) => record(`onChangePlan → ${plan.code}`)}
            onCancel={() => record("onCancel")}
            onResume={() => record("onResume")}
            onWithdrawPendingPlan={() => record("onWithdrawPendingPlan")}
            onPaid={(s) => record(`onPaid → ${s.sessionId}`)}
            onLeaveCheckoutReturn={() => record("onLeaveCheckoutReturn")}
          />
        ) : (
          <CheckoutOutcome
            key={`${run}-${ending}-${reason}`}
            sessionId="sess_playground"
            variant={hero ? "hero" : "banner"}
            fetchSession={fakeFetchSession(ending, reason)}
            pollIntervalMs={600}
            timeoutMs={4000}
            onPaid={(s) => record(`onPaid → ${s.sessionId}`)}
            onContinue={(s) => record(`onContinue → ${s.status}`)}
            onRetry={(s) => record(`onRetry → ${s.status}${s.failureReason ? ` (${s.failureReason})` : ""}`)}
            onDismiss={(s) => record(`onDismiss → ${s.status}`)}
          />
        )}
      </main>

      <footer className="mx-auto max-w-5xl px-6 pb-10">
        <h2 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Ce que le composant renvoie
        </h2>
        <ul className="space-y-1 font-mono text-xs text-muted-foreground">
          {log.length === 0 ? <li>—</li> : log.map((l, i) => <li key={i}>{l}</li>)}
        </ul>
      </footer>
    </div>
  )
}

function Tab({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
        active ? "bg-foreground text-background" : "border border-input hover:bg-muted"
      }`}
    >
      {children}
    </button>
  )
}

function Toggle({
  checked,
  onChange,
  children,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  children: React.ReactNode
}) {
  return (
    <label className="inline-flex items-center gap-2">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {children}
    </label>
  )
}

createRoot(document.getElementById("root")!).render(<App />)
