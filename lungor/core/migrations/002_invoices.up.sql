-- Invoices we ISSUE ourselves.
--
-- Until now the settings screen told the customer their invoices were "gérées
-- par notre prestataire de paiement". That was never quite true: Mollie's
-- Invoices API returns the invoices MOLLIE sends US for its fees, and what the
-- customer receives is a payment confirmation email — not a legal invoice. It
-- carries no sequential number, no seller identification, no VAT statement.
-- Selling B2B in France means someone has to issue a real document, and that
-- someone is us.
--
-- The shape mirrors Lungor's finance schema so that moving billing there later
-- is a change of implementation, not a data migration (see lib/invoicing). Two
-- deliberate departures:
--
--   * Lungor scopes rows by tenant_id/customer_id/app_id, all FKs into tables
--     that do not exist here (tenants, customers, apps). Techtuel IS the single
--     tenant with a single app, so those become plain columns carrying the
--     configured constants — same contract, no phantom tables. owner_id is the
--     real scoping key, matching subscriptions, usage_ledger and
--     customer_emails.
--
--   * The seller and buyer identity is SNAPSHOT on the row. An invoice is a
--     frozen legal document: if the customer later changes their name, country
--     or VAT number, past invoices must keep what was true on the issue date.
--     Reading it back through a join would silently rewrite history.
--
-- No FK to accounts, for the same reason customer_emails has none (000023):
-- the record must survive an account deletion. French law requires invoices to
-- be kept for 10 years; a cascade delete would destroy that obligation.

CREATE TABLE IF NOT EXISTS invoices (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    TEXT NOT NULL,

    -- Lungor scoping ids, carried as configured constants. Nullable so the
    -- table works before they are configured; they become real ids at swap.
    tenant_id   UUID,
    app_id      UUID,

    -- The legal invoice number. UNIQUE because a number identifies exactly one
    -- document, forever: reusing one is fraud, not a bug.
    number      TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL CHECK (status IN ('draft','open','paid','void','uncollectible')),

    -- Money in integer minor units. NEVER float: a cent lost to binary
    -- rounding is a cent that fails an audit.
    subtotal    BIGINT NOT NULL DEFAULT 0,
    tax_amount  BIGINT NOT NULL DEFAULT 0,
    total       BIGINT NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT 'EUR',

    -- Tax as APPLIED, not as recomputed. Under franchise en base (CGI 293 B)
    -- the rate is 0 and tax_regime records WHY, so a future move to a VAT-
    -- registered regime cannot retroactively reinterpret old invoices as
    -- 20%-rated documents that under-collected.
    tax_country TEXT,
    tax_rate    NUMERIC(5,4) NOT NULL DEFAULT 0,
    tax_regime  TEXT NOT NULL DEFAULT 'franchise',
    -- The statutory mention printed on the document (e.g. the 293 B wording).
    legal_mention TEXT NOT NULL DEFAULT '',

    -- Buyer identity, snapshot at issue time.
    customer_name    TEXT NOT NULL DEFAULT '',
    customer_email   TEXT NOT NULL DEFAULT '',
    customer_country TEXT NOT NULL DEFAULT '',
    customer_vat_id  TEXT NOT NULL DEFAULT '',

    -- Seller identity, snapshot at issue time. Our own SIREN and address can
    -- change (a move, a legal form change); the document must not.
    seller_name    TEXT NOT NULL DEFAULT '',
    seller_siren   TEXT NOT NULL DEFAULT '',
    seller_vat_id  TEXT NOT NULL DEFAULT '',
    seller_address TEXT[] NOT NULL DEFAULT '{}',

    -- The billing period covered. Bounds the subscription month being charged.
    period_start TIMESTAMPTZ,
    period_end   TIMESTAMPTZ,
    due_at       TIMESTAMPTZ,
    paid_at      TIMESTAMPTZ,
    issued_at    TIMESTAMPTZ,

    -- The Mollie payment that settled this invoice (tr_xxx). We issue the
    -- document; Mollie still moves the money.
    provider_invoice_id TEXT,

    -- Object-storage key of the generated Factur-X PDF. Not a URL: the bucket
    -- host can change, and the PDF is served through our own authenticated
    -- endpoint, never linked publicly.
    pdf_key TEXT NOT NULL DEFAULT '',

    -- False for invoices issued in test mode. Carried for Lungor parity.
    livemode   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The customer-facing listing: "my invoices, newest first".
CREATE INDEX IF NOT EXISTS idx_invoices_owner
    ON invoices (owner_id, issued_at DESC);

-- Makes ClosePeriod idempotent at the DATABASE level, not just in code. A
-- retried period close (a crash between issuing and committing, a duplicate
-- webhook) must not allocate a second invoice number for the same month —
-- and an application-level check would race under concurrency.
CREATE UNIQUE INDEX IF NOT EXISTS idx_invoices_owner_period
    ON invoices (owner_id, period_start, period_end)
    WHERE period_start IS NOT NULL AND status <> 'void';

CREATE TABLE IF NOT EXISTS invoice_lines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('flat','usage')),
    description TEXT NOT NULL,
    unit        TEXT,                       -- usage unit code for kind='usage'
    quantity    BIGINT NOT NULL DEFAULT 1,
    unit_amount BIGINT NOT NULL,            -- minor units, VAT-exclusive
    amount      BIGINT NOT NULL,            -- quantity * unit_amount
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice
    ON invoice_lines (invoice_id);

-- Invoice numbering. French law requires an unbroken, chronological sequence
-- with no gaps, which is why the counter lives in the database and is bumped
-- inside the issuing transaction rather than derived from COUNT(*) — a deleted
-- or voided row must not shift every subsequent number.
--
-- Keyed by year: the sequence restarts at 1 each January, giving numbers of the
-- form 2026-0001. Rows are never deleted.
CREATE TABLE IF NOT EXISTS invoice_sequences (
    year     INT PRIMARY KEY,
    last_seq BIGINT NOT NULL DEFAULT 0
);
