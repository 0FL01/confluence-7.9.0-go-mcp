# Confluence 7.9.0 MCP

Target: **only Confluence 7.9.0 (server, selfhosted)**. Data Center and Cloud are not compatibility targets.

## Map
- `main.go`: `run`/`main` bootstrap, `loadConfig` configuration, `setupServer` tool schemas, and `handle*` handlers.
- `main.go`: `ConfluenceClient.executeRequest`, `doRequest`, and `getJSON` own REST transport and response handling.
- `main_test.go`: handler/client tests with `httptest`; `TestRun` injects a serve function without starting stdio.
- `.github/workflows/ci.yml`: Go version and CI checks.
- `.github/workflows/release.yml`: six cross-platform artifacts and checksums; local builds use `go build` (no build script).

## Rules
- Keep stdout reserved for MCP protocol traffic; diagnostics belong on stderr.
- When changing the public tool contract, update schemas in `setupServer`, handlers, tests, and README tool descriptions together.
- Handler validation/API failures use `mcp.NewToolResultError(...), nil`; successful results contain response JSON as text.
- Route REST calls through client helpers to retain request context, Bearer authentication, and the 30-second timeout; close response bodies.
- Preserve URL environment precedence: `CONFLUENCE_BASE_URL`, then `CONFLUENCE_API_BASE_PATH`, then `CONFLUENCE_HOST`; retain context paths when adding `/rest/api`.
- Page bodies use Confluence `storage` representation, not Markdown.
- Updates fetch current content first, preserve type/space and unchanged title/body, and default version to current + 1; incomplete reused fields must fail before PUT, not become empty replacements.
- Get/update handlers reject content IDs containing `/` or `..` before issuing requests.
- MCP numeric arguments are currently decoded as `float64`; direct handler tests use that representation.

## Verify
Use Go 1.25.5 as specified by `go.mod` and CI; run commands from the repository root.
- `go vet ./...`
- `go test -v -race -coverprofile=coverage.out -covermode=atomic ./...`
- `go build -v ./...`
- Focused example: `go test -v -run '^TestHandleUpdateContent' .`
- Configuration tests isolate all four `CONFLUENCE_*` variables; never use production credentials for tests.

## Docs
- `README.md`: current tool arguments, configuration, installation, and pinned Server 7.9.0 REST/CQL references.
