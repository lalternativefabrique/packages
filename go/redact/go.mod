module github.com/lalternative/packages/go/redact

go 1.25.0

require (
	github.com/go-pdf/fpdf v0.9.0
	github.com/lalternative/packages/go/docfacts v0.1.0
	github.com/lalternative/packages/go/pdftext v0.2.0
)

require github.com/ledongthuc/pdf v0.0.0-20261020125853-5e46805f718e // indirect

replace (
	github.com/lalternative/packages/go/docfacts => ../docfacts
	github.com/lalternative/packages/go/pdftext => ../pdftext
)
