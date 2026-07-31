package local_test

// Integration suite for the half of invoicing that only exists in SQL.
//
// These cannot be asserted against a fake. The properties that matter here are
// properties of a UNIQUE INDEX, an UPDATE ... RETURNING and a transaction
// boundary — not of Go control flow:
//
//   - numbering is gapless and allocated under a row lock, because French law
//     requires an unbroken chronological sequence;
//   - a period closes at most once, because an invoice number is legally
//     irrevocable and a re-delivered webhook must not mint a second;
//   - a customer can never read another customer's invoice, which is a WHERE
//     clause, not a check in a handler.
//
// Override the DB with TRANSCRIPT_TEST_DATABASE_URL / DATABASE_URL. Skips
// (never a false green) when no database is reachable.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/lungor/core/invoicing"
	"github.com/lalternative/packages/lungor/core/invoicing/local"
)

// ownerA/ownerB namespace this suite's rows. Packages share one database, so a
// suite that deleted rows another owns would race it.
const (
	ownerA = "invoicing-it-owner-a"
	ownerB = "invoicing-it-owner-b"
)

// memPDFs is an in-memory PDFStore. The document bytes are not what this suite
// asserts — facturx has its own tests — but ClosePeriod must still be able to
// store something, and a failing store must not lose the invoice.
type memPDFs struct {
	mu      sync.Mutex
	objects map[string][]byte
	failing bool
}

func newMemPDFs() *memPDFs { return &memPDFs{objects: map[string][]byte{}} }

func (m *memPDFs) Upload(_ context.Context, key string, body io.Reader, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failing {
		return errors.New("storage unavailable")
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[key] = b
	return nil
}

func (m *memPDFs) Download(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func testSeller() invoicing.Seller {
	return invoicing.Seller{
		Name:    "L'Alternative",
		Country: "FR",
		Regime:  invoicing.RegimeFranchise,
		SIREN:   "823762653",
		Address: []string{"8 route de la Taoulère", "64400 Eysus"},
		Email:   "noreply@techtuel.com",
	}
}

func newStore(t *testing.T) (*local.Store, *pgxpool.Pool, *memPDFs) {
	t.Helper()
	dsn := os.Getenv("TRANSCRIPT_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("no database configured (TRANSCRIPT_TEST_DATABASE_URL / DATABASE_URL)")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database unreachable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(ctx,
			`DELETE FROM invoices WHERE owner_id = ANY($1)`,
			[]string{ownerA, ownerB})
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})

	pdfs := newMemPDFs()
	return local.New(pool, pdfs, testSeller()), pool, pdfs
}

func monthInput(owner string, month time.Month, amount int64) invoicing.ClosePeriodInput {
	start := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
	return invoicing.ClosePeriodInput{
		CustomerID:      owner,
		CustomerName:    owner + "@example.com",
		CustomerEmail:   owner + "@example.com",
		CustomerCountry: "FR",
		Currency:        "EUR",
		PeriodStart:     start.Format(time.RFC3339),
		PeriodEnd:       start.AddDate(0, 1, 0).Format(time.RFC3339),
		Lines: []invoicing.ClosePeriodLine{{
			Kind: invoicing.LineFlat, Description: "Techtuel Pro",
			Quantity: 1, UnitAmount: amount,
		}},
	}
}

// A re-delivered webhook must not mint a second invoice number: a number is
// legally irrevocable once allocated.
func TestClosePeriod_IsIdempotentPerPeriod(t *testing.T) {
	s, pool, _ := newStore(t)
	ctx := context.Background()

	first, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900))
	if err != nil {
		t.Fatalf("first close: %v", err)
	}

	for i := 0; i < 3; i++ {
		again, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900))
		if err != nil {
			t.Fatalf("replay %d: %v", i+1, err)
		}
		if again.ID != first.ID {
			t.Fatalf("replay %d issued a NEW invoice %s (want the existing %s)",
				i+1, again.Number, first.Number)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE owner_id = $1`, ownerA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d invoice rows after 4 closes, want 1", n)
	}
}

// Concurrency is the case an application-level check cannot cover: two webhook
// deliveries landing at once must still produce ONE invoice.
func TestClosePeriod_ConcurrentClosesIssueOneInvoice(t *testing.T) {
	s, pool, _ := newStore(t)
	ctx := context.Background()

	const racers = 6
	var wg sync.WaitGroup
	ids := make([]string, racers)
	errs := make([]error, racers)
	start := make(chan struct{})

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together to make the race real
			inv, err := s.ClosePeriod(ctx, monthInput(ownerA, time.August, 900))
			ids[i], errs[i] = inv.ID, err
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v", i, err)
		}
		seen[ids[i]] = true
	}
	if len(seen) != 1 {
		t.Errorf("concurrent closes produced %d distinct invoices, want 1", len(seen))
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE owner_id = $1`, ownerA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d invoice rows after %d concurrent closes, want 1", n, racers)
	}
}

// French law wants an unbroken chronological sequence. Numbers must increment
// by exactly one, and be shaped YYYY-NNNN.
func TestClosePeriod_NumbersAreSequentialAndGapless(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()

	var numbers []string
	for _, m := range []time.Month{time.January, time.February, time.March} {
		inv, err := s.ClosePeriod(ctx, monthInput(ownerA, m, 900))
		if err != nil {
			t.Fatalf("close %v: %v", m, err)
		}
		numbers = append(numbers, inv.Number)
	}

	year := time.Now().UTC().Year()
	var prev int
	for i, num := range numbers {
		var y, seq int
		if _, err := fmt.Sscanf(num, "%d-%d", &y, &seq); err != nil {
			t.Fatalf("number %q is not YYYY-NNNN: %v", num, err)
		}
		if y != year {
			t.Errorf("number %q carries year %d, want %d", num, y, year)
		}
		if i > 0 && seq != prev+1 {
			t.Errorf("sequence jumped: %q follows seq %d (gap)", num, prev)
		}
		prev = seq
	}
}

// A customer must never read another's invoice, and a foreign id must be
// indistinguishable from a missing one — otherwise the endpoint is a probe.
func TestGetInvoice_ForeignInvoiceIsNotFound(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()

	mine, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900))
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := s.GetInvoice(ctx, ownerB, mine.ID); !errors.Is(err, invoicing.ErrInvoiceNotFound) {
		t.Errorf("owner B reading owner A's invoice: err = %v, want ErrInvoiceNotFound", err)
	}
	if _, err := s.GetInvoicePDF(ctx, ownerB, mine.ID); !errors.Is(err, invoicing.ErrInvoiceNotFound) {
		t.Errorf("owner B downloading owner A's PDF: err = %v, want ErrInvoiceNotFound", err)
	}
	// A malformed id must take the same branch, not surface a scan error.
	if _, err := s.GetInvoice(ctx, ownerA, "not-a-uuid"); !errors.Is(err, invoicing.ErrInvoiceNotFound) {
		t.Errorf("malformed id: err = %v, want ErrInvoiceNotFound", err)
	}
}

// The listing is the customer-facing surface: it must never leak across owners,
// and an unscoped call must return nothing rather than everything.
func TestListInvoices_ScopedToTheCaller(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClosePeriod(ctx, monthInput(ownerB, time.July, 1900)); err != nil {
		t.Fatal(err)
	}

	listA, err := s.ListInvoices(ctx, invoicing.ListParams{CustomerID: ownerA})
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 {
		t.Fatalf("owner A sees %d invoices, want 1", len(listA))
	}
	if listA[0].Amount != 900 {
		t.Errorf("owner A sees amount %d — that is owner B's invoice", listA[0].Amount)
	}

	// An empty customer id must list NOTHING. Returning everything here would
	// hand one customer the whole book.
	all, err := s.ListInvoices(ctx, invoicing.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("unscoped list returned %d invoices, want 0", len(all))
	}
}

// Totals must be computed from the lines, not trusted from the caller, and
// under franchise en base the tax is zero with the total equal to the subtotal.
func TestClosePeriod_TotalsComeFromTheLines(t *testing.T) {
	s, _, _ := newStore(t)
	ctx := context.Background()

	in := monthInput(ownerA, time.July, 900)
	in.Lines = []invoicing.ClosePeriodLine{
		{Kind: invoicing.LineFlat, Description: "Techtuel Pro", Quantity: 1, UnitAmount: 900},
		{Kind: invoicing.LineUsage, Description: "Crédits", Unit: "credit", Quantity: 3, UnitAmount: 50},
	}

	inv, err := s.ClosePeriod(ctx, in)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if want := int64(900 + 150); inv.Subtotal != want {
		t.Errorf("subtotal = %d, want %d", inv.Subtotal, want)
	}
	if inv.TaxAmount != 0 {
		t.Errorf("tax = %d, want 0 under franchise en base", inv.TaxAmount)
	}
	if inv.Total != inv.Subtotal {
		t.Errorf("total %d != subtotal %d with no VAT", inv.Total, inv.Subtotal)
	}
	if len(inv.Lines) != 2 {
		t.Fatalf("%d lines persisted, want 2", len(inv.Lines))
	}
}

// An incomplete seller must refuse to issue rather than produce a document that
// looks valid and is not — the failure would otherwise surface at an audit.
func TestClosePeriod_RefusesWithoutSellerIdentity(t *testing.T) {
	dsn := os.Getenv("TRANSCRIPT_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("no database configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("database unreachable: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database unreachable: %v", err)
	}

	seller := testSeller()
	seller.SIREN = "" // the state before the SIREN is configured
	s := local.New(pool, newMemPDFs(), seller)

	if _, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900)); !errors.Is(err, invoicing.ErrSellerIncomplete) {
		t.Errorf("err = %v, want ErrSellerIncomplete", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE owner_id = $1`, ownerA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d invoice rows written despite the refusal, want 0", n)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM invoices WHERE owner_id = $1`, ownerA)
}

// A storage outage must not lose an invoice that is legally already issued. The
// row stands with no pdf_key; only the download is unavailable.
func TestClosePeriod_StorageFailureStillIssuesTheInvoice(t *testing.T) {
	s, pool, pdfs := newStore(t)
	ctx := context.Background()
	pdfs.failing = true

	inv, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900))
	if err != nil {
		t.Fatalf("close failed on a storage outage: %v — the invoice is issued either way", err)
	}
	if inv.Number == "" {
		t.Error("invoice has no number")
	}

	var pdfKey string
	if err := pool.QueryRow(ctx,
		`SELECT pdf_key FROM invoices WHERE id = $1`, inv.ID).Scan(&pdfKey); err != nil {
		t.Fatal(err)
	}
	if pdfKey != "" {
		t.Errorf("pdf_key = %q after a failed upload, want empty", pdfKey)
	}
	if _, err := s.GetInvoicePDF(ctx, ownerA, inv.ID); err == nil {
		t.Error("GetInvoicePDF returned a document that was never stored")
	}
}

// The stored document must be the real thing: a PDF/A-3 that round-trips
// through storage intact. A truncated or text-mangled blob would still
// "download" and only fail when a customer opens it.
//
// The 293 B mention is NOT asserted by scanning the raw bytes: page content and
// the embedded CII are FlateDecode streams, so a substring search finds nothing
// even when the mention is present — a false failure that would push someone to
// "fix" working code. The mention is covered where it is readable, on the CII
// itself (facturx.TestFranchiseMentionInBothLayers). What only this layer can
// prove is that the bytes survived the round trip, so that is what it checks.
func TestClosePeriod_StoresARealFacturXPDF(t *testing.T) {
	s, _, pdfs := newStore(t)
	ctx := context.Background()

	inv, err := s.ClosePeriod(ctx, monthInput(ownerA, time.July, 900))
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	doc, err := s.GetInvoicePDF(ctx, ownerA, inv.ID)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.HasPrefix(doc, []byte("%PDF")) {
		head := doc
		if len(head) > 8 {
			head = head[:8]
		}
		t.Errorf("stored object is not a PDF (starts with %q)", head)
	}
	// A PDF is only complete if it ends with its EOF marker: a truncated upload
	// keeps the %PDF header and is unopenable.
	if !bytes.Contains(doc[max(0, len(doc)-64):], []byte("%%EOF")) {
		t.Error("stored PDF has no EOF trailer — truncated")
	}
	// The Factur-X XML must be ATTACHED, which is what makes it Factur-X rather
	// than a plain PDF. The attachment's filename is not compressed.
	if !bytes.Contains(doc, []byte("factur-x.xml")) {
		t.Error("stored PDF does not embed factur-x.xml — not a Factur-X document")
	}

	// The bytes must be identical to what was uploaded: storage must not
	// re-encode them.
	pdfs.mu.Lock()
	var stored []byte
	for _, b := range pdfs.objects {
		stored = b
	}
	pdfs.mu.Unlock()
	if !bytes.Equal(doc, stored) {
		t.Errorf("downloaded %d bytes, stored %d — the document was altered in transit",
			len(doc), len(stored))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
