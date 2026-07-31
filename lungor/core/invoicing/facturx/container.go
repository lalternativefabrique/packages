package facturx

import (
	"bytes"
	_ "embed"
	"fmt"
	"time"

	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// sRGB ICC profile embedded into the binary: the PDF/A-3 OutputIntent
// destination profile. Embedding keeps the product self-contained — no system
// ICC dependency — so it deploys as an immutable, sovereign container.
//
//go:embed assets/sRGB-v2-micro.icc
var srgbICC []byte

// xmlFileName is the mandatory attachment name for the EN 16931 CII profile.
const xmlFileName = "factur-x.xml"

// buildPDFA3 takes a plain invoice PDF and the CII XML and produces a Factur-X
// container: PDF/A-3 conformance markers (OutputIntent + embedded sRGB ICC +
// XMP declaring pdfaid:part=3 and the fx: extension) plus the XML embedded as an
// associated file (/AF + /Filespec with /AFRelationship /Alternative, also in
// the EmbeddedFiles name tree).
//
// NOTE: conformance is built to spec but is PROVEN only by the veraPDF CI gate,
// not by this code or local runs.
func buildPDFA3(pdfIn, xml []byte, invoiceNumber string, issuedAt time.Time) ([]byte, error) {
	ctx, err := pdfcpu.ReadContext(bytes.NewReader(pdfIn), model.NewDefaultConfiguration())
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}
	xt := ctx.XRefTable

	catalog, err := xt.Catalog()
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}

	// 1) Embedded file stream for the XML.
	efRef, err := xt.NewEmbeddedStreamDict(bytes.NewReader(xml), issuedAt)
	if err != nil {
		return nil, fmt.Errorf("embedded stream: %w", err)
	}

	// 2) Filespec for the associated file, then add the Factur-X-required
	//    AFRelationship + the EmbeddedFile subtype.
	fsDict, err := xt.NewFileSpecDict(xmlFileName, xmlFileName, "Factur-X EN 16931 invoice data", *efRef)
	if err != nil {
		return nil, fmt.Errorf("filespec: %w", err)
	}
	fsDict["AFRelationship"] = types.Name("Alternative")
	fsRef, err := xt.IndRefForNewObject(fsDict)
	if err != nil {
		return nil, err
	}

	// 3) Catalog /AF array (document-level associated files).
	catalog["AF"] = types.Array{*fsRef}

	// 4) EmbeddedFiles name tree (compatibility with non-AF-aware readers).
	if err := xt.LocateNameTree("EmbeddedFiles", true); err != nil {
		return nil, fmt.Errorf("locate name tree: %w", err)
	}
	nm := model.NameMap{xmlFileName: []types.Dict{fsDict}}
	if err := xt.Names["EmbeddedFiles"].Add(xt, xmlFileName, *fsRef, nm, []string{"F", "UF"}); err != nil {
		return nil, fmt.Errorf("name tree add: %w", err)
	}

	// 5) OutputIntent + embedded sRGB ICC stream.
	if err := addOutputIntent(xt, catalog); err != nil {
		return nil, err
	}

	// 6) Document XMP metadata (pdfaid:part=3 + fx extension).
	if err := addXMPMetadata(xt, catalog, invoiceNumber); err != nil {
		return nil, err
	}

	catalog["Version"] = types.Name("1.7")

	var out bytes.Buffer
	if err := pdfcpu.WriteContext(ctx, &out); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return out.Bytes(), nil
}

// addOutputIntent adds an /OutputIntents array with a GTS_PDFA1 intent whose
// DestOutputProfile is the embedded sRGB ICC stream.
func addOutputIntent(xt *model.XRefTable, catalog types.Dict) error {
	iccSD, err := xt.NewStreamDictForBuf(srgbICC)
	if err != nil {
		return fmt.Errorf("icc stream: %w", err)
	}
	iccSD.InsertInt("N", 3) // RGB
	if err := iccSD.Encode(); err != nil {
		return fmt.Errorf("encode icc: %w", err)
	}
	iccRef, err := xt.IndRefForNewObject(*iccSD)
	if err != nil {
		return err
	}
	oi := types.NewDict()
	oi["Type"] = types.Name("OutputIntent")
	oi["S"] = types.Name("GTS_PDFA1")
	oi["OutputConditionIdentifier"] = types.StringLiteral("sRGB")
	oi["Info"] = types.StringLiteral("sRGB IEC61966-2.1")
	oi["DestOutputProfile"] = *iccRef
	oiRef, err := xt.IndRefForNewObject(oi)
	if err != nil {
		return err
	}
	catalog["OutputIntents"] = types.Array{*oiRef}
	return nil
}

// addXMPMetadata attaches the document-level XMP packet (uncompressed, as PDF/A
// requires the Metadata stream to be plain).
func addXMPMetadata(xt *model.XRefTable, catalog types.Dict, invoiceNumber string) error {
	packet := []byte(facturXMP(invoiceNumber))
	// Build a well-formed stream dict, then strip the Flate filter: PDF/A
	// requires the XMP Metadata stream to be plain (uncompressed). Encode()
	// computes Raw/Length; with no filter pipeline it stores the bytes verbatim.
	sd, err := xt.NewStreamDictForBuf(packet)
	if err != nil {
		return fmt.Errorf("xmp stream: %w", err)
	}
	sd.Dict["Type"] = types.Name("Metadata")
	sd.Dict["Subtype"] = types.Name("XML")
	sd.Dict.Delete("Filter")
	sd.FilterPipeline = nil
	if err := sd.Encode(); err != nil {
		return fmt.Errorf("encode xmp: %w", err)
	}
	ref, err := xt.IndRefForNewObject(*sd)
	if err != nil {
		return err
	}
	catalog["Metadata"] = *ref
	return nil
}
