module github.com/lalternative/packages/lungor/entitlement

go 1.25.0

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/labstack/echo/v4 v4.15.2
	github.com/lalternative/packages/lungor/policy v0.0.0-20260812154134-093becf20cf2
	github.com/lalternative/packages/lungor/sdk-go v0.9.0
	golang.org/x/sync v0.20.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/oapi-codegen/runtime v1.6.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace github.com/lalternative/packages/lungor/policy => ../policy

replace github.com/lalternative/packages/lungor/sdk-go => ../sdk-go
