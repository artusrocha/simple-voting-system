#!/bin/sh
set -e

cd /typespec

if [ ! -d "node_modules" ]; then
    echo "Installing TypeSpec dependencies..."
    npm install @typespec/compiler @typespec/http @typespec/rest @typespec/openapi3
else
    echo "Dependencies already installed"
fi

echo "Starting TypeSpec compiler in watch mode..."
exec tsp compile . --output-dir /openapi --emit @typespec/openapi3 --watch
