#!/usr/bin/env sh
# Refresh openapi/lungor.json from a running Lungor, then regenerate the wire
# types.
#
# This used to be `cp` from a private checkout, which is why the file sat three
# routes behind for months: nothing forces a copy to be refreshed, and a stale
# one produces a client that compiles, passes its tests, and cannot call the
# endpoints it is missing. Lungor serves its own contract now, so there is no
# checkout to have.
#
# Usage: ./refresh-contract.sh [base-url]
set -eu

BASE="${1:-${LUNGOR_BASE_URL:-https://api.lungor.fr}}"
URL="${BASE%/}/openapi.json"
OUT="$(dirname "$0")/openapi/lungor.json"

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
# line of a 3000-line document, so a run that changed nothing would still
# produce a diff nobody can read — and a real change would hide in it.

if [ -f "$OUT" ] && cmp -s "$TMP" "$OUT"; then
	echo "contract unchanged"
	exit 0
fi

mv "$TMP" "$OUT"
trap - EXIT
echo "contract updated — regenerating"
cd "$(dirname "$0")" && go generate ./...
echo "done; reconcile any compile error the new shape causes"
