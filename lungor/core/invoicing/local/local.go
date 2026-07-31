// Package local serves the [invoicing.Client] contract in-process: it issues
// invoices from our own database and generates the Factur-X document itself.
//
// It is one of two interchangeable implementations. The other, added when
// billing moves to Lungor, will be an HTTP client against the same contract.
// Nothing outside this package knows which one is in use — that is the whole
// point of the interface, and the reason this package exports no type beyond
// the constructor's return.
package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/lungor/core/invoicing"
	"github.com/lalternative/packages/lungor/core/invoicing/facturx"
)

// PDFStore is the subset of object storage this package needs. Declared here
// rather than imported so the package does not depend on the whole storage
// surface — and so tests can substitute an in-memory store.
type PDFStore interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// Store issues and reads invoices backed by Postgres + object storage.
type Store struct {
	pool   *pgxpool.Pool
	pdfs   PDFStore
	seller invoicing.Seller
	// now is injectable so tests can pin the issue date and the year the
	// numbering sequence keys on.
	now func() time.Time
}

// New returns a Store. The seller is fixed configuration: it identifies the
// legal entity issuing every invoice.
func New(pool *pgxpool.Pool, pdfs PDFStore, seller invoicing.Seller) *Store {
	return &Store{pool: pool, pdfs: pdfs, seller: seller, now: time.Now}
}

var _ invoicing.Client = (*Store)(nil)

// ListInvoices returns one customer's invoices, newest first.
//
// An empty CustomerID returns nothing rather than everything: this method backs
// a customer-facing endpoint, and a caller that forgets to scope it must get an
// empty list, never someone else's invoices.
func (s *Store) ListInvoices(ctx context.Context, p invoicing.ListParams) ([]invoicing.ListItem, error) {
	if p.CustomerID == "" {
		return []invoicing.ListItem{}, nil
	}
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	// Drafts are excluded: an invoice that has not been issued is an internal
	// artefact, and showing it would let a customer see a document that may
	// still change or be discarded.
	const q = `
		SELECT id::text, number, customer_name, customer_email,
		       total, currency, status, issued_at
		  FROM invoices
		 WHERE owner_id = $1 AND status <> 'draft'
		 ORDER BY issued_at DESC NULLS LAST, created_at DESC
		 LIMIT $2`

	rows, err := s.pool.Query(ctx, q, p.CustomerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	out := make([]invoicing.ListItem, 0, 8)
	for rows.Next() {
		var it invoicing.ListItem
		var issued *time.Time
		if err := rows.Scan(&it.ID, &it.Number, &it.CustomerName, &it.CustomerEmail,
			&it.Amount, &it.Currency, &it.Status, &issued); err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		if issued != nil {
			v := issued.Format(time.RFC3339)
			it.IssuedAt = &v
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	return out, nil
}

// GetInvoice returns one invoice with its lines.
//
// The owner is part of the WHERE clause, not checked after loading: an invoice
// belonging to someone else must be indistinguishable from one that does not
// exist, otherwise the endpoint becomes a probe for other customers' invoice
// ids.
func (s *Store) GetInvoice(ctx context.Context, customerID, id string) (invoicing.Invoice, error) {
	const q = `
		SELECT id::text, number, status, subtotal, tax_amount, total, currency,
		       tax_country, tax_rate, period_start, period_end, due_at, paid_at,
		       issued_at, customer_name, customer_email, pdf_key, livemode,
		       created_at
		  FROM invoices
		 WHERE id = $1 AND owner_id = $2 AND status <> 'draft'`

	var inv invoicing.Invoice
	var taxCountry *string
	var periodStart, periodEnd, dueAt, paidAt, issuedAt *time.Time
	var pdfKey string
	var createdAt time.Time

	err := s.pool.QueryRow(ctx, q, id, customerID).Scan(
		&inv.ID, &inv.Number, &inv.Status, &inv.Subtotal, &inv.TaxAmount,
		&inv.Total, &inv.Currency, &taxCountry, &inv.TaxRate,
		&periodStart, &periodEnd, &dueAt, &paidAt, &issuedAt,
		&inv.CustomerName, &inv.CustomerEmail, &pdfKey, &inv.Livemode, &createdAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return invoicing.Invoice{}, invoicing.ErrInvoiceNotFound
	case err != nil:
		// An invalid uuid string reaches here as a scan/parse error; report it
		// as not-found so a malformed id is not distinguishable from a foreign
		// one either.
		return invoicing.Invoice{}, invoicing.ErrInvoiceNotFound
	}

	if taxCountry != nil {
		inv.TaxCountry = *taxCountry
	}
	inv.PeriodStart = rfc3339(periodStart)
	inv.PeriodEnd = rfc3339(periodEnd)
	inv.DueAt = rfc3339(dueAt)
	inv.PaidAt = rfc3339(paidAt)
	inv.IssuedAt = rfc3339(issuedAt)
	inv.CreatedAt = createdAt.Format(time.RFC3339)
	if pdfKey != "" {
		// Served through our own authenticated endpoint, never as a bucket URL.
		inv.PDFURL = "/v1/invoices/" + inv.ID + "/pdf"
	}

	lines, err := s.lines(ctx, inv.ID)
	if err != nil {
		return invoicing.Invoice{}, err
	}
	inv.Lines = lines
	return inv, nil
}

func (s *Store) lines(ctx context.Context, invoiceID string) ([]invoicing.Line, error) {
	const q = `
		SELECT id::text, kind, description, COALESCE(unit,''),
		       quantity, unit_amount, amount
		  FROM invoice_lines
		 WHERE invoice_id = $1
		 ORDER BY created_at, id`

	rows, err := s.pool.Query(ctx, q, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list lines: %w", err)
	}
	defer rows.Close()

	out := make([]invoicing.Line, 0, 4)
	for rows.Next() {
		var l invoicing.Line
		if err := rows.Scan(&l.ID, &l.Kind, &l.Description, &l.Unit,
			&l.Quantity, &l.UnitAmount, &l.Amount); err != nil {
			return nil, fmt.Errorf("scan line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetInvoicePDF returns the stored Factur-X document.
func (s *Store) GetInvoicePDF(ctx context.Context, customerID, id string) ([]byte, error) {
	const q = `SELECT pdf_key FROM invoices
	            WHERE id = $1 AND owner_id = $2 AND status <> 'draft'`

	var key string
	err := s.pool.QueryRow(ctx, q, id, customerID).Scan(&key)
	if err != nil || key == "" {
		return nil, invoicing.ErrInvoiceNotFound
	}

	rc, err := s.pdfs.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("download invoice pdf: %w", err)
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, fmt.Errorf("read invoice pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.Format(time.RFC3339)
	return &v
}

// pdfKey is the object-storage key for an invoice document. Scoped by owner so
// account deletion can sweep a prefix, and by year so the bucket stays browsable.
func pdfKey(ownerID, year, number string) string {
	return fmt.Sprintf("invoices/%s/%s/%s.pdf", ownerID, year, number)
}

// facturxInput maps the persisted invoice and the configured seller onto the
// neutral generator contract.
func (s *Store) facturxInput(inv invoicing.Invoice, in invoicing.ClosePeriodInput, issuedAt time.Time) facturx.Input {
	rate := s.seller.Rate(in.CustomerCountry)

	lines := make([]facturx.Line, 0, len(in.Lines))
	for _, l := range in.Lines {
		lines = append(lines, facturx.Line{
			Description: l.Description,
			Quantity:    l.Quantity,
			UnitAmount:  l.UnitAmount,
			VATRate:     rate,
		})
	}

	return facturx.Input{
		Number:    inv.Number,
		Currency:  inv.Currency,
		IssueDate: issuedAt.Format("20060102"),
		Seller: facturx.Party{
			Name:    s.seller.Name,
			VATID:   s.seller.VATID,
			LegalID: s.seller.SIREN,
			Address: s.seller.Address,
			Country: s.seller.Country,
			Email:   s.seller.Email,
		},
		Buyer: facturx.Party{
			Name:    in.CustomerName,
			VATID:   in.CustomerVATID,
			Country: in.CustomerCountry,
			Email:   in.CustomerEmail,
		},
		LegalMention: s.seller.LegalMention(),
		PeriodStart:  compactDate(in.PeriodStart),
		PeriodEnd:    compactDate(in.PeriodEnd),
		Lines:        lines,
	}
}

// compactDate converts an RFC3339 timestamp to the YYYYMMDD form CII wants.
// Returns "" on anything unparseable, which makes the period simply absent
// rather than malformed.
func compactDate(rfc string) string {
	if rfc == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return ""
	}
	return t.Format("20060102")
}
