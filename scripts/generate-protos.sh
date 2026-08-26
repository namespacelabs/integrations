#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/.."

INTERNAL="${NS_INTERNAL:-../internal}"
if [[ ! -d "$INTERNAL/public/proto" ]]; then
	echo "error: internal checkout not found at $INTERNAL (set NS_INTERNAL)" >&2
	exit 1
fi

exec buf generate "$INTERNAL/public" \
	--exclude-path "$INTERNAL/public/proto/namespace/cloud/integrations/bazel/v1beta"
