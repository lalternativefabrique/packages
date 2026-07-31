package facturx

import (
	"encoding/xml"
	"fmt"
	"sort"
)

// money formats minor units as a decimal string with 2 fraction digits.
func money(minor int64) string {
	neg := ""
	if minor < 0 {
		neg = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%d.%02d", neg, minor/100, minor%100)
}

// percent formats VAT basis points (2000 = 20%) as a decimal string.
func percent(bp int32) string {
	return fmt.Sprintf("%d.%02d", bp/100, bp%100)
}

// GenerateCII maps an Input to its EN 16931 CII document.
func GenerateCII(in Input) (*CrossIndustryInvoice, error) {
	if !in.issuable() {
		return nil, ErrIncomplete
	}

	lines := make([]LineItem, len(in.Lines))
	for i, l := range in.Lines {
		lines[i] = LineItem{
			Doc:     LineDocument{LineID: fmt.Sprintf("%d", i+1)},
			Product: TradeProduct{Name: l.Description},
			Agreement: LineAgreement{
				NetPrice: Amount{Value: money(l.UnitAmount)},
			},
			Delivery: LineDelivery{
				Quantity: Quantity{UnitCode: "C62", Value: fmt.Sprintf("%d", l.Quantity)},
			},
			Settlement: LineSettlement{
				Tax: LineTax{
					TypeCode:     "VAT",
					CategoryCode: vatCategory(l.VATRate),
					RatePercent:  percent(l.VATRate),
				},
				LineTotal: Amount{Value: money(l.lineTotal())},
			},
		}
	}

	seller := tradeParty(in.Seller)
	buyer := tradeParty(in.Buyer)

	doc := &CrossIndustryInvoice{
		XMLNSRSM: nsRSM,
		XMLNSRAM: nsRAM,
		XMLNSUDT: nsUDT,
		Context: ExchangedDocumentContext{
			GuidelineParameter: GuidelineParameter{ID: guidelineEN16931},
		},
		Document: ExchangedDocument{
			ID:        in.Number,
			TypeCode:  typeCodeCommercialInvoice,
			IssueDate: FormattedDate{Format: "102", Value: in.IssueDate},
			Notes:     documentNotes(in),
		},
		Transaction: SupplyChainTradeTransaction{
			Lines:     lines,
			Agreement: HeaderTradeAgreement{Seller: seller, Buyer: buyer},
			Settlement: HeaderTradeSettlement{
				Currency:     in.Currency,
				TaxBreakdown: vatBreakdown(in),
				Period:       billingPeriod(in),
				Summation: MonetarySummation{
					LineTotal:     Amount{Value: money(in.totalExclVAT())},
					TaxBasisTotal: Amount{Value: money(in.totalExclVAT())},
					TaxTotal:      &AmountWithCurrency{Currency: in.Currency, Value: money(in.totalVAT())},
					GrandTotal:    Amount{Value: money(in.totalInclVAT())},
					DuePayable:    Amount{Value: money(in.totalInclVAT())},
				},
			},
		},
	}
	return doc, nil
}

// tradeParty maps a neutral Party to a CII TradeParty. The VAT number is carried
// as the legal-organization ID under the EU VAT scheme (ISO 6523 / 9930),
// the European-standard identifier the EN 16931 profile expects.
func tradeParty(p Party) TradeParty {
	// Name (BT-27 / BT-44) is MANDATORY in EN 16931: a party with an empty name
	// makes the document invalid. So where the PDF may omit an unknown buyer
	// name, the CII falls back to the email — the only identity we hold for a
	// customer who never gave a billing name.
	name := p.Name
	if name == "" {
		name = p.Email
	}
	tp := TradeParty{Name: name}
	switch {
	case p.VATID != "":
		tp.LegalOrg = &LegalOrganization{ID: SchemedID{SchemeID: schemeVAT, Value: p.VATID}}
	case p.LegalID != "":
		// A seller under franchise en base has no VAT number at all, so the
		// country-local registration (SIREN) is the ONLY identifier available.
		// Without this branch such a party would be emitted anonymously.
		scheme := p.LegalIDScheme
		if scheme == "" {
			scheme = schemeSIREN
		}
		tp.LegalOrg = &LegalOrganization{ID: SchemedID{SchemeID: scheme, Value: p.LegalID}}
	}
	if len(p.Address) > 0 || p.Country != "" {
		tp.Address = &TradeAddress{CountryID: p.Country}
		for i, line := range p.Address {
			switch {
			case i == 0:
				tp.Address.LineOne = line
			case i == 1:
				tp.Address.LineTwo = line
			case i == 2:
				tp.Address.LineThree = line
			default:
				// Beyond three lines CII has no slot; append rather than drop,
				// so a long address stays complete on the document.
				tp.Address.LineThree += ", " + line
			}
		}
	}
	if p.Email != "" {
		tp.Contact = &TradeContact{Email: &URIUniversal{URIID: p.Email}}
	}
	return tp
}

// documentNotes carries the seller's statutory mention into the structured data
// (BT-22). Nil when there is none, so a VAT-registered seller emits no note.
func documentNotes(in Input) []DocumentNote {
	if in.LegalMention == "" {
		return nil
	}
	return []DocumentNote{{Content: in.LegalMention}}
}

// billingPeriod maps the covered service period (BG-14). Nil unless BOTH bounds
// are set: a half-open period is not a period, and CII requires both dates.
func billingPeriod(in Input) *BillingPeriod {
	if in.PeriodStart == "" || in.PeriodEnd == "" {
		return nil
	}
	return &BillingPeriod{
		Start: FormattedDate{Format: "102", Value: in.PeriodStart},
		End:   FormattedDate{Format: "102", Value: in.PeriodEnd},
	}
}

// vatCategory maps a rate to its EN 16931 category code (BT-118). A zero rate
// is NOT category "S" at 0%: it is "E" (exempt), and a document that claims
// standard-rated 0% VAT is semantically wrong even though the arithmetic works.
func vatCategory(rateBp int32) string {
	if rateBp == 0 {
		return categoryExempt
	}
	return categoryStandard
}

// vatBreakdown aggregates lines into one TradeTax per VAT rate (BG-23), as
// EN 16931 requires a VAT breakdown per category/rate.
func vatBreakdown(in Input) []TradeTax {
	type acc struct{ basis, tax int64 }
	byRate := map[int32]*acc{}
	for _, l := range in.Lines {
		a := byRate[l.VATRate]
		if a == nil {
			a = &acc{}
			byRate[l.VATRate] = a
		}
		a.basis += l.lineTotal()
		a.tax += l.lineVAT()
	}
	rates := make([]int, 0, len(byRate))
	for r := range byRate {
		rates = append(rates, int(r))
	}
	sort.Ints(rates)

	out := make([]TradeTax, 0, len(rates))
	for _, r := range rates {
		a := byRate[int32(r)]
		t := TradeTax{
			CalculatedAmount: Amount{Value: money(a.tax)},
			TypeCode:         "VAT",
			BasisAmount:      Amount{Value: money(a.basis)},
			CategoryCode:     vatCategory(int32(r)),
			RatePercent:      percent(int32(r)),
		}
		// EN 16931 (BR-E-10) requires an exemption reason whenever a breakdown
		// line is category E. The seller's statutory mention is that reason.
		if t.CategoryCode == categoryExempt && in.LegalMention != "" {
			t.ExemptionReason = in.LegalMention
		}
		out = append(out, t)
	}
	return out
}

// Marshal renders the CII document as XML bytes with the standard declaration.
func Marshal(doc *CrossIndustryInvoice) ([]byte, error) {
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// GenerateXML is the convenience one-shot: Input -> CII XML bytes.
func GenerateXML(in Input) ([]byte, error) {
	doc, err := GenerateCII(in)
	if err != nil {
		return nil, err
	}
	return Marshal(doc)
}
