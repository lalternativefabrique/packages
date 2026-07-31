package facturx

import "time"

// GenerateFacturX produces a complete Factur-X file from an Input: the
// human-readable PDF with the EN 16931 CII XML embedded in a PDF/A-3 container.
// Returns ErrIncomplete when the Input lacks a number or issue date.
//
// Conformance is built to spec; it is PROVEN by the veraPDF CI gate, not here.
func GenerateFacturX(in Input) ([]byte, error) {
	if !in.issuable() {
		return nil, ErrIncomplete
	}
	xml, err := GenerateXML(in)
	if err != nil {
		return nil, err
	}
	pdf, err := renderInvoicePDF(in)
	if err != nil {
		return nil, err
	}
	issuedAt := parseIssueDate(in.IssueDate)
	return buildPDFA3(pdf, xml, in.Number, issuedAt)
}

// parseIssueDate parses the YYYYMMDD issue date for the embedded-file timestamp.
// On any parse failure it falls back to the zero time — the value only stamps
// the PDF attachment's mod date, not the legal invoice date (which is in the
// XML/PDF body), so a bad value must not fail document generation.
func parseIssueDate(yyyymmdd string) time.Time {
	t, err := time.Parse("20060102", yyyymmdd)
	if err != nil {
		return time.Time{}
	}
	return t
}
