#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME="${RUNTIME:-podman}"
GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.23-bookworm}"

run_test() {
  local service_dir="$1"
  "$RUNTIME" run --rm \
    -v "$PROJECT_ROOT/$service_dir:/src:Z" \
    -w /src \
    --entrypoint sh \
    "$GO_IMAGE" \
    -lc 'apt-get update >/dev/null && apt-get install -y --no-install-recommends build-essential pkg-config >/dev/null && /usr/local/go/bin/go mod download && /usr/local/go/bin/go mod verify && /usr/local/go/bin/go test ./...'
}

run_test "apps/api"
run_test "apps/projector"

printf '[go-test-container] verified and tested api and projector using %s\n' "$GO_IMAGE"
