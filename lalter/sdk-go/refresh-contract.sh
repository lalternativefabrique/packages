#!/usr/bin/env sh
# Refresh openapi/lalter.json from a running lalter core, then regenerate the
# wire types.
#
# Without this, the file would have to be a manual copy out of a checkout —
# and a copy nobody is forced to refresh is one that goes stale silently.
# lalter serves its own contract, so there is no checkout to have.
#
# Usage: ./refresh-contract.sh [base-url]
set -eu

BASE="${1:-${LALTER_BASE_URL:-https://api.lalter.fr}}"
URL="${BASE%/}/openapi.json"
OUT="$(dirname "$0")/openapi/lalter.json"

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "fetching $URL"
curl -fsSL "$URL" -o "$TMP"

# The contract is public but the response is not trusted blindly: a captive
# portal or an error page returned with 200 would otherwise overwrite the
# contract with something that is not one.
if ! head -c 512 "$TMP" | grep -q '"openapi"'; then
	echo "refusing to install a document with no openapi version: $URL" >&2
	exit 1
fi

# Installed byte for byte, deliberately unformatted: reindenting rewrites every
# line of the document, so a run that changed nothing would still produce a
# diff nobody can read — and a real change would hide in it.

if [ -f "$OUT" ] && cmp -s "$TMP" "$OUT"; then
	echo "contract unchanged"
	exit 0
fi

mv "$TMP" "$OUT"
trap - EXIT
echo "contract updated — regenerating"
cd "$(dirname "$0")" && go generate ./...
echo "done; reconcile any compile error the new shape causes"
