#!/usr/bin/env sh
# Publish one Spore SDK from a clean main.
#
# Usage: ./spore/publish.sh python   (needs UV_PUBLISH_TOKEN, a PyPI API token)
#        ./spore/publish.sh php      (needs push access to SPORE_PHP_REMOTE)
#
# The Node SDK publishes with `npm publish` from spore/sdk-node.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ "$(git rev-parse --abbrev-ref HEAD)" != main ]; then
	echo "publish only from main" >&2
	exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
	echo "publish only from a clean tree" >&2
	exit 1
fi

case "${1:-}" in
python)
	: "${UV_PUBLISH_TOKEN:?set UV_PUBLISH_TOKEN to a PyPI API token scoped to spore-email}"
	VERSION="$(sed -n 's/^version = "\(.*\)"$/\1/p' spore/sdk-python/pyproject.toml)"
	if curl -fsS -o /dev/null "https://pypi.org/pypi/spore-email/$VERSION/json"; then
		echo "spore-email $VERSION is already on PyPI" >&2
		exit 1
	fi
	OUT="$(mktemp -d)"
	trap 'rm -rf "$OUT"' EXIT
	uv build spore/sdk-python --out-dir "$OUT"
	uv publish "$OUT"/*
	;;
php)
	REMOTE="${SPORE_PHP_REMOTE:-git@github.com:lalternativefabrique/spore-php.git}"
	VERSION="$(sed -n 's/^ *"version": "\(.*\)",$/\1/p' spore/sdk-php/composer.json)"
	SPLIT="$(git subtree split --prefix=spore/sdk-php HEAD)"
	git push "$REMOTE" "$SPLIT:refs/heads/main"
	git push "$REMOTE" "$SPLIT:refs/tags/v$VERSION"
	;;
*)
	echo "usage: $0 python|php" >&2
	exit 2
	;;
esac
