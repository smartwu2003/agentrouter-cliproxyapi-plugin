# agentrouter plugin source

Build with `make build` (or the repository-level `build.ps1` / `build.sh`).

| File | Purpose |
| --- | --- |
| `main.go` | C ABI entry points, lifecycle, capability registration |
| `config.go` | YAML configuration and protocol names |
| `catalog.go` | Curated model table, live discovery, protocol selection, check-in scheduler |
| `executor.go` | Executor, protocol translation, streaming relay |
| `route.go` | `model_router` decisions |
| `account.go` | Dashboard login, check-in, account info |
| `management.go` | Management routes and the embedded browser page |
| `host.go` | Host callbacks (stream emit/close, logging) |
| `http_client.go` | Control-plane HTTP client with optional proxy |
| `json_util.go` | Payload helpers |
