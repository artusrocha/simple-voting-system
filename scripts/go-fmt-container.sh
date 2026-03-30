#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME="${RUNTIME:-podman}"
GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.23-bookworm}"

run_fmt() {
  local service_dir="$1"
  "$RUNTIME" run --rm \
    -v "$PROJECT_ROOT/$service_dir:/src:Z" \
    -w /src \
    --entrypoint sh \
    "$GO_IMAGE" \
    -lc '/usr/local/go/bin/gofmt -w $(find . -name "*.go" -type f)'
}

run_fmt "apps/api"
run_fmt "apps/projector"

printf '[go-fmt-container] formatted api and projector Go files using %s\n' "$GO_IMAGE"
