#!/usr/bin/env bash
# Build the AgentRouter plugin as a CLIProxyAPI native plugin for the current
# platform. Requires Go 1.26+ and a C toolchain.
set -euo pipefail
cd "$(dirname "$0")/plugin"

case "$(go env GOOS)" in
  windows) ext="dll" ;;
  darwin)  ext="dylib" ;;
  *)       ext="so" ;;
esac

output="${1:-../dist/agentrouter.$ext}"
mkdir -p "$(dirname "$output")"

CGO_ENABLED=1 go test ./...
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -ldflags="-s -w" -o "$output" .
echo "built $output ($(go env GOOS)/$(go env GOARCH))"
