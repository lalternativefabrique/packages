package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lalternative/packages/lungor/core/invoicing"
	"github.com/lalternative/packages/lungor/core/invoicing/facturx"
)

// ClosePeriod issues the invoice for one billing period.
//
// Two properties matter more than anything else here, and both are enforced by
// the database rather than by careful calling:
//
//   - IDEMPOTENCE. A retry — a crashed worker, a redelivered webhook, an
//     operator running the close twice — must return the existing invoice, not
//     mint a second one. An invoice number is legally irrevocable once
//     allocated, so "issue it again" is not a recoverable mistake.
//
//   - AN UNBROKEN SEQUENCE. French law requires chronological numbering with no
//     gaps. The counter is bumped inside the same transaction that inserts the
//     invoice, so a failure after numbering rolls the number back and leaves no
//     hole. Deriving the number from COUNT(*) would instead renumber history
//     every time a row was voided.
//
// The PDF is generated AFTER the transaction commits. Generating it inside
// would hold a row lock across CPU-bound work, and a storage outage would then
// roll back an invoice that is legally already issued. A row whose pdf_key is
// empty is a document that exists and can be regenerated; the reverse — a PDF
// for an invoice that was rolled back — is not recoverable.
func (s *Store) ClosePeriod(ctx context.Context, in invoicing.ClosePeriodInput) (invoicing.Invoice, error) {
	if in.CustomerID == "" {
		return invoicing.Invoice{}, errors.New("invoicing: customer id required")
	}
	// Refuse to issue rather than produce a document that looks valid and is
	// not. A missing SIREN or address only surfaces at an audit otherwise —
	// long after the invoices went out.
	if err := s.seller.Validate(); err != nil {
		return invoicing.Invoice{}, err
	}

	issuedAt := s.now().UTC()
	periodStart, err := parseTime(in.PeriodStart)
	if err != nil {
		return invoicing.Invoice{}, fmt.Errorf("period start: %w", err)
	}
	periodEnd, err := parseTime(in.PeriodEnd)
	if err != nil {
		return invoicing.Invoice{}, fmt.Errorf("period end: %w", err)
	}

	currency := in.Currency
	if currency == "" {
		currency = "EUR"
	}

	// Totals are computed from the lines, never taken from the caller: the
	// document must agree with what it itemises.
	rate := s.seller.Rate(in.CustomerCountry)
	var subtotal int64
	for _, l := range in.Lines {
		subtotal += l.Quantity * l.UnitAmount
	}
	tax := subtotal * int64(rate) / 10000
	total := subtotal + tax

	// Already issued for this period? Return it unchanged. Checked BEFORE the
	// transaction, against committed state: an invoice from a previous run is
	// what a retry is looking for, and doing it here keeps the common path to a
	// single query that allocates no number and holds no lock. A close racing
	// concurrently is not caught here — the unique index below catches that.
	if existing, err := s.getForPeriod(ctx, in.CustomerID, periodStart, periodEnd); err == nil {
		return existing, nil
	} else if !errors.Is(err, invoicing.ErrInvoiceNotFound) {
		return invoicing.Invoice{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return invoicing.Invoice{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	number, err := nextNumber(ctx, tx, issuedAt.Year())
	if err != nil {
		return invoicing.Invoice{}, err
	}

	const ins = `
		INSERT INTO invoices (
			owner_id, tenant_id, app_id, number, status,
			subtotal, tax_amount, total, currency,
			tax_country, tax_rate, tax_regime, legal_mention,
			customer_name, customer_email, customer_country, customer_vat_id,
			seller_name, seller_siren, seller_vat_id, seller_address,
			period_start, period_end, issued_at, livemode
		) VALUES (
			$1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, $4, 'open',
			$5, $6, $7, $8,
			$9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22, $23, TRUE
		)
		RETURNING id::text`

	var id string
	err = tx.QueryRow(ctx, ins,
		in.CustomerID, s.seller.TenantID, s.seller.AppID, number,
		subtotal, tax, total, currency,
		in.CustomerCountry, float64(rate)/10000, string(s.seller.Regime), s.seller.LegalMention(),
		in.CustomerName, in.CustomerEmail, in.CustomerCountry, in.CustomerVATID,
		s.seller.Name, s.seller.SIREN, s.seller.VATID, s.seller.Address,
		periodStart, periodEnd, issuedAt,
	).Scan(&id)
	if err != nil {
		// Lost a race against a concurrent close: the unique index on
		// (owner_id, period) rejected the second insert. That is the index
		// doing its job — recover by returning the invoice that won.
		if isUniqueViolation(err) {
			tx.Rollback(ctx)
			return s.getForPeriod(ctx, in.CustomerID, periodStart, periodEnd)
		}
		return invoicing.Invoice{}, fmt.Errorf("insert invoice: %w", err)
	}

	for _, l := range in.Lines {
		const insLine = `
			INSERT INTO invoice_lines (invoice_id, kind, description, unit,
			                           quantity, unit_amount, amount)
			VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7)`
		kind := l.Kind
		if kind == "" {
			kind = invoicing.LineFlat
		}
		if _, err := tx.Exec(ctx, insLine, id, string(kind), l.Description,
			l.Unit, l.Quantity, l.UnitAmount, l.Quantity*l.UnitAmount); err != nil {
			return invoicing.Invoice{}, fmt.Errorf("insert line: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return invoicing.Invoice{}, fmt.Errorf("commit: %w", err)
	}

	inv, err := s.GetInvoice(ctx, in.CustomerID, id)
	if err != nil {
		return invoicing.Invoice{}, err
	}

	// Best-effort: the invoice is issued either way. A failure here leaves a
	// row with no pdf_key, which regenerates on demand rather than losing a
	// legally-issued document to a storage hiccup.
	if key, err := s.storePDF(ctx, inv, in, issuedAt); err == nil {
		const upd = `UPDATE invoices SET pdf_key = $2, updated_at = NOW() WHERE id = $1`
		if _, err := s.pool.Exec(ctx, upd, id, key); err == nil {
			inv.PDFURL = "/v1/invoices/" + id + "/pdf"
		}
	}
	return inv, nil
}

// storePDF generates the Factur-X document and uploads it.
func (s *Store) storePDF(ctx context.Context, inv invoicing.Invoice, in invoicing.ClosePeriodInput, issuedAt time.Time) (string, error) {
	doc, err := facturx.GenerateFacturX(s.facturxInput(inv, in, issuedAt))
	if err != nil {
		return "", fmt.Errorf("generate facturx: %w", err)
	}
	key := pdfKey(in.CustomerID, issuedAt.Format("2006"), inv.Number)
	if err := s.pdfs.Upload(ctx, key, bytes.NewReader(doc), "application/pdf"); err != nil {
		return "", fmt.Errorf("upload invoice pdf: %w", err)
	}
	return key, nil
}

// nextNumber allocates the next number in the year's sequence.
//
// The UPDATE ... RETURNING is atomic and takes a row lock for the rest of the
// transaction, so two concurrent closes serialise here instead of both reading
// the same counter. The upsert seeds the year's first invoice.
func nextNumber(ctx context.Context, tx pgx.Tx, year int) (string, error) {
	const q = `
		INSERT INTO invoice_sequences (year, last_seq) VALUES ($1, 1)
		ON CONFLICT (year) DO UPDATE SET last_seq = invoice_sequences.last_seq + 1
		RETURNING last_seq`

	var seq int64
	if err := tx.QueryRow(ctx, q, year).Scan(&seq); err != nil {
		return "", fmt.Errorf("allocate invoice number: %w", err)
	}
	return fmt.Sprintf("%d-%04d", year, seq), nil
}

// getForPeriod finds the invoice already issued for a period, if any. Returns
// ErrInvoiceNotFound when there is none.
func (s *Store) getForPeriod(ctx context.Context, ownerID string, start, end *time.Time) (invoicing.Invoice, error) {
	const q = `
		SELECT id::text FROM invoices
		 WHERE owner_id = $1 AND period_start = $2 AND period_end = $3
		   AND status <> 'void'`

	var id string
	if err := s.pool.QueryRow(ctx, q, ownerID, start, end).Scan(&id); err != nil {
		return invoicing.Invoice{}, invoicing.ErrInvoiceNotFound
	}
	return s.GetInvoice(ctx, ownerID, id)
}

// MarkPaid records settlement of an invoice by a provider payment.
func (s *Store) MarkPaid(ctx context.Context, invoiceID, providerPaymentID string, paidAt time.Time) error {
	const q = `
		UPDATE invoices
		   SET status = 'paid', paid_at = $2, provider_invoice_id = $3,
		       updated_at = NOW()
		 WHERE id = $1 AND status <> 'void'`

	_, err := s.pool.Exec(ctx, q, invoiceID, paidAt, providerPaymentID)
	if err != nil {
		return fmt.Errorf("mark invoice paid: %w", err)
	}
	return nil
}

func parseTime(rfc string) (*time.Time, error) {
	if rfc == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

// isUniqueViolation reports a Postgres 23505. Matched on the SQLSTATE rather
// than the message so it survives a locale change.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
