#!/usr/bin/env sh
# Refresh the checked-in Spore OpenAPI 3 contract from a running API, then
# regenerate every generated SDK from that exact document.
#
# Usage: ./spore/refresh-contract.sh [base-url]
set -eu

BASE="${1:-${SPORE_BASE_URL:-https://api.sporee.fr}}"
URL="${BASE%/}/openapi.json"
OUT="$(dirname "$0")/openapi.json"

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "fetching $URL"
curl -fsSL "$URL" -o "$TMP"

# Do not replace the SDK contract with an HTML error page, a captive portal or
# the old Swagger 2 document. SDK generation is standardized on OpenAPI 3.
if ! head -c 512 "$TMP" | grep -q '"openapi"'; then
	echo "refusing to install a document with no OpenAPI 3 version: $URL" >&2
	exit 1
fi

if [ -f "$OUT" ] && cmp -s "$TMP" "$OUT"; then
	echo "contract unchanged"
	exit 0
fi

mv "$TMP" "$OUT"
trap - EXIT
echo "contract updated — regenerating all SDKs"
cd "$(dirname "$0")/.."
pnpm --filter @lalternative/spore-codegen generate
echo "done; review and test the generated changes"
