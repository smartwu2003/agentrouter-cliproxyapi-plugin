# Build helper for the AgentRouter plugin.
#
# The plugin must be compiled against the same CLIProxyAPI SDK generation the
# host was built with. Replace CORE_VERSION with the version reported by
# `cliproxyapi --version` / `core-version.txt` in EasyCLIProxyAPI.
CORE_VERSION ?= v8.0.36
GOOS  ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ifeq ($(GOOS),windows)
EXT := dll
else ifeq ($(GOOS),darwin)
EXT := dylib
else
EXT := so
endif

OUT ?= ../dist/agentrouter_$(GOOS)_$(GOARCH).$(EXT)

.PHONY: all sync test build clean

all: sync test build

sync:
	go mod edit -require=github.com/router-for-me/CLIProxyAPI/v8@$(CORE_VERSION)
	go mod tidy

test:
	CGO_ENABLED=1 go test ./...

build:
	mkdir -p $(dir $(OUT))
	CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -ldflags="-s -w" -o $(OUT) .
	@echo "built $(OUT)"

clean:
	rm -rf ../dist
