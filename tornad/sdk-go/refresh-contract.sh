#!/usr/bin/env sh
# Refresh openapi/tornad.json from a running tornad, then regenerate the wire
# types.
#
# The client this replaces was written by hand against tornad's source, which is
# why it covered /search alone and never learned that /map and /crawl exist:
# nothing forces a hand-written client to be refreshed, and a stale one compiles,
# passes its tests, and cannot call the endpoints it is missing. Tornad serves
# its own contract now, so there is no source to read.
#
# Usage: ./refresh-contract.sh [base-url]
set -eu

BASE="${1:-${TORNAD_BASE_URL:-http://app.tornad-production.internal}}"
URL="${BASE%/}/openapi.json"
OUT="$(dirname "$0")/openapi/tornad.json"

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "fetching $URL"
curl -fsSL "$URL" -o "$TMP"

# The contract is not trusted blindly: a captive portal or an error page
# returned with 200 would otherwise overwrite it with something that is not one.
if ! head -c 512 "$TMP" | grep -q '"openapi"'; then
	echo "refusing to install a document with no openapi version: $URL" >&2
	exit 1
fi

# Installed byte for byte, deliberately unformatted: reindenting rewrites every
# line, so a run that changed nothing would still produce a diff nobody can
# read — and a real change would hide in it.

if [ -f "$OUT" ] && cmp -s "$TMP" "$OUT"; then
	echo "contract unchanged"
	exit 0
fi

mv "$TMP" "$OUT"
trap - EXIT
echo "contract updated — regenerating"
cd "$(dirname "$0")" && go generate ./...
echo "done; reconcile any compile error the new shape causes"
