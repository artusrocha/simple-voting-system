#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONTRACTS_DIR="$PROJECT_ROOT/contracts/http"
OUTPUT_DIR="$CONTRACTS_DIR/generated"

echo "Generating OpenAPI specification..."

mkdir -p "$OUTPUT_DIR"

if command -v tsp &> /dev/null; then
    echo "Using tsp to generate OpenAPI..."
    cd "$CONTRACTS_DIR/typespec"
    tsp compile main.tsp --emit @typespec/openapi3:"$OUTPUT_DIR/openapi.json"
    echo "OpenAPI spec generated at $OUTPUT_DIR/openapi.json"
else
    echo "WARNING: tsp not found. Please install @typespec/compiler:"
    echo "  npm install -g @typespec/tsp-compiler"
    echo "  or"
    echo "  npm install @typespec/compiler"
    echo ""
    echo "For now, creating placeholder file..."
    echo '{}' > "$OUTPUT_DIR/openapi.json"
    exit 1
fi
