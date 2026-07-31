// CII (Cross Industry Invoice) XML model — the legal-core half of Factur-X.
//
// The structs below model the subset of UN/CEFACT CII (D16B) required by the
// EN 16931 profile. Field/namespace names follow the CII schema so the output
// validates; only elements we populate are modeled. The PDF/A-3 container that
// embeds this XML is assembled in container.go.
package facturx

import "encoding/xml"

// Namespaces used by the CII document (UN/CEFACT D16B, EN 16931 profile).
const (
	nsRSM = "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"
	nsRAM = "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"
	nsUDT = "urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100"

	// guidelineEN16931 is the Factur-X "guideline" id for the EN 16931 profile.
	guidelineEN16931 = "urn:cen.eu:en16931:2017"

	// typeCodeCommercialInvoice is UNTDID 1001 code 380 (commercial invoice).
	typeCodeCommercialInvoice = "380"
	// schemeVAT is the ISO 6523 scheme id for an intra-EU VAT number (9930).
	// We identify parties by VAT number (the European-standard identifier)
	// rather than a country-local SIREN (0002).
	schemeVAT = "9930"
	// schemeSIREN is the ISO 6523 scheme id for a French SIREN (0002). Used as
	// the party identifier when there is no VAT number to carry.
	schemeSIREN = "0002"

	// EN 16931 VAT category codes (UNTDID 5305, BT-118).
	categoryStandard = "S" // standard rated
	categoryExempt   = "E" // exempt from VAT — requires an exemption reason
)

// CrossIndustryInvoice is the CII document root.
type CrossIndustryInvoice struct {
	XMLName  xml.Name `xml:"rsm:CrossIndustryInvoice"`
	XMLNSRSM string   `xml:"xmlns:rsm,attr"`
	XMLNSRAM string   `xml:"xmlns:ram,attr"`
	XMLNSUDT string   `xml:"xmlns:udt,attr"`

	Context     ExchangedDocumentContext    `xml:"rsm:ExchangedDocumentContext"`
	Document    ExchangedDocument           `xml:"rsm:ExchangedDocument"`
	Transaction SupplyChainTradeTransaction `xml:"rsm:SupplyChainTradeTransaction"`
}

type ExchangedDocumentContext struct {
	GuidelineParameter GuidelineParameter `xml:"ram:GuidelineSpecifiedDocumentContextParameter"`
}

type GuidelineParameter struct {
	ID string `xml:"ram:ID"`
}

// ExchangedDocument is the invoice header (number, type, issue date).
type ExchangedDocument struct {
	ID        string        `xml:"ram:ID"`       // invoice number
	TypeCode  string        `xml:"ram:TypeCode"` // 380
	IssueDate FormattedDate `xml:"ram:IssueDateTime>udt:DateTimeString"`
	// Notes (BT-22) carry free-text statements about the document — here the
	// seller's statutory mention, so the exemption is stated in the structured
	// data and not only in the human-readable PDF layer.
	Notes []DocumentNote `xml:"ram:IncludedNote,omitempty"`
}

type DocumentNote struct {
	Content string `xml:"ram:Content"`
}

// FormattedDate carries a date with its format code (102 = YYYYMMDD).
type FormattedDate struct {
	Format string `xml:"format,attr"`
	Value  string `xml:",chardata"`
}

type SupplyChainTradeTransaction struct {
	Lines      []LineItem            `xml:"ram:IncludedSupplyChainTradeLineItem"`
	Agreement  HeaderTradeAgreement  `xml:"ram:ApplicableHeaderTradeAgreement"`
	Delivery   HeaderTradeDelivery   `xml:"ram:ApplicableHeaderTradeDelivery"`
	Settlement HeaderTradeSettlement `xml:"ram:ApplicableHeaderTradeSettlement"`
}

// LineItem is one invoice line (BG-25).
type LineItem struct {
	Doc        LineDocument   `xml:"ram:AssociatedDocumentLineDocument"`
	Product    TradeProduct   `xml:"ram:SpecifiedTradeProduct"`
	Agreement  LineAgreement  `xml:"ram:SpecifiedLineTradeAgreement"`
	Delivery   LineDelivery   `xml:"ram:SpecifiedLineTradeDelivery"`
	Settlement LineSettlement `xml:"ram:SpecifiedLineTradeSettlement"`
}

type LineDocument struct {
	LineID string `xml:"ram:LineID"`
}

type TradeProduct struct {
	Name string `xml:"ram:Name"`
}

type LineAgreement struct {
	NetPrice Amount `xml:"ram:NetPriceProductTradePrice>ram:ChargeAmount"`
}

type LineDelivery struct {
	Quantity Quantity `xml:"ram:BilledQuantity"`
}

type LineSettlement struct {
	Tax       LineTax `xml:"ram:ApplicableTradeTax"`
	LineTotal Amount  `xml:"ram:SpecifiedTradeSettlementLineMonetarySummation>ram:LineTotalAmount"`
}

type LineTax struct {
	TypeCode     string `xml:"ram:TypeCode"`     // VAT
	CategoryCode string `xml:"ram:CategoryCode"` // S (standard)
	RatePercent  string `xml:"ram:RateApplicablePercent"`
}

// HeaderTradeAgreement carries seller + buyer (BG-4 / BG-7).
type HeaderTradeAgreement struct {
	Seller TradeParty `xml:"ram:SellerTradeParty"`
	Buyer  TradeParty `xml:"ram:BuyerTradeParty"`
}

type TradeParty struct {
	Name     string             `xml:"ram:Name"`
	LegalOrg *LegalOrganization `xml:"ram:SpecifiedLegalOrganization,omitempty"`
	Contact  *TradeContact      `xml:"ram:DefinedTradeContact,omitempty"`
	Address  *TradeAddress      `xml:"ram:PostalTradeAddress,omitempty"`
}

// TradeAddress is a party's postal address (BG-5 / BG-8). CII names each line
// separately (LineOne/Two/Three) rather than repeating one element, so the
// configured display lines are mapped onto those three slots; anything beyond
// the third is folded into LineThree rather than dropped. CountryID (BT-40 /
// BT-55) is the only element EN 16931 makes mandatory here.
type TradeAddress struct {
	LineOne   string `xml:"ram:LineOne,omitempty"`
	LineTwo   string `xml:"ram:LineTwo,omitempty"`
	LineThree string `xml:"ram:LineThree,omitempty"`
	CountryID string `xml:"ram:CountryID,omitempty"`
}

type LegalOrganization struct {
	ID SchemedID `xml:"ram:ID"`
}

type SchemedID struct {
	SchemeID string `xml:"schemeID,attr"`
	Value    string `xml:",chardata"`
}

type TradeContact struct {
	Email *URIUniversal `xml:"ram:EmailURIUniversalCommunication,omitempty"`
}

type URIUniversal struct {
	URIID string `xml:"ram:URIID"`
}

type HeaderTradeDelivery struct{} // empty for services; present per schema

// HeaderTradeSettlement carries currency, VAT breakdown and the monetary
// summation (BG-22).
type HeaderTradeSettlement struct {
	Currency     string            `xml:"ram:InvoiceCurrencyCode"`
	TaxBreakdown []TradeTax        `xml:"ram:ApplicableTradeTax"`
	Period       *BillingPeriod    `xml:"ram:BillingSpecifiedPeriod,omitempty"`
	Summation    MonetarySummation `xml:"ram:SpecifiedTradeSettlementHeaderMonetarySummation"`
}

type TradeTax struct {
	CalculatedAmount Amount `xml:"ram:CalculatedAmount"`
	TypeCode         string `xml:"ram:TypeCode"` // VAT
	// ExemptionReason (BT-120) states WHY no VAT applies. EN 16931 rule BR-E-10
	// makes it mandatory on an exempt breakdown line; omitted when rated.
	ExemptionReason string `xml:"ram:ExemptionReason,omitempty"`
	BasisAmount     Amount `xml:"ram:BasisAmount"`
	CategoryCode    string `xml:"ram:CategoryCode"` // S or E
	RatePercent     string `xml:"ram:RateApplicablePercent"`
}

// BillingPeriod bounds the service period the invoice covers (BG-14). It is
// what makes a subscription invoice self-explanatory: without it the reader
// cannot tell which month the charge is for.
type BillingPeriod struct {
	Start FormattedDate `xml:"ram:StartDateTime>udt:DateTimeString"`
	End   FormattedDate `xml:"ram:EndDateTime>udt:DateTimeString"`
}

type MonetarySummation struct {
	LineTotal     Amount              `xml:"ram:LineTotalAmount"`
	TaxBasisTotal Amount              `xml:"ram:TaxBasisTotalAmount"`
	TaxTotal      *AmountWithCurrency `xml:"ram:TaxTotalAmount,omitempty"`
	GrandTotal    Amount              `xml:"ram:GrandTotalAmount"`
	DuePayable    Amount              `xml:"ram:DuePayableAmount"`
}

// Amount is a monetary value rendered as a decimal string.
type Amount struct {
	Value string `xml:",chardata"`
}

// AmountWithCurrency is a monetary value carrying its currency (required on
// TaxTotalAmount).
type AmountWithCurrency struct {
	Currency string `xml:"currencyID,attr"`
	Value    string `xml:",chardata"`
}

// Quantity is a billed quantity with a unit code (C62 = "one"/piece).
type Quantity struct {
	UnitCode string `xml:"unitCode,attr"`
	Value    string `xml:",chardata"`
}
