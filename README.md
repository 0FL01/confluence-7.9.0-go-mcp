# Confluence 7.9.0 Server MCP (Go)

[![Go Version](https://img.shields.io/badge/Go-1.25.5-blue.svg)](https://golang.org)
[![MCP Go SDK](https://img.shields.io/badge/mcp--go-0.43.2-green.svg)](https://github.com/mark3labs/mcp-go)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A stdio Model Context Protocol (MCP) server for **self-hosted Atlassian Confluence Server 7.9.0**. It lets MCP clients retrieve and search content, create pages and blog posts, update content, and list or search spaces. Data Center and Cloud are not compatibility targets.

The canonical repository and Go module are [`github.com/0FL01/confluence-7.9.0-go-mcp`](https://github.com/0FL01/confluence-7.9.0-go-mcp). The MCP server name is `confluence-7.9.0-go-mcp`, and its application version is `1.0.0`.

## Installation and MCP Configuration

### Build from source

Source builds are the primary installation method. Use **Go 1.25.5**, the version specified by `go.mod` and GitHub Actions.

```bash
git clone https://github.com/0FL01/confluence-7.9.0-go-mcp.git
cd confluence-7.9.0-go-mcp
go build ./...
```

This produces `confluence-7.9.0-go-mcp` in the repository root (`confluence-7.9.0-go-mcp.exe` on Windows). Configure your MCP client with the absolute path to that binary:

```json
{
  "mcpServers": {
    "confluence": {
      "command": "/absolute/path/to/confluence-7.9.0-go-mcp",
      "env": {
        "CONFLUENCE_API_TOKEN": "your-server-personal-access-token",
        "CONFLUENCE_BASE_URL": "https://confluence.example.com/confluence"
      }
    }
  }
}
```

The server communicates over stdio. Standard output is reserved for MCP protocol messages; diagnostics go to standard error. For development, you can also run `go run .` from the repository root with the same environment variables set.

### Prebuilt binaries (when available)

No release has been published under the canonical repository yet. Once a release is available, download the appropriate artifact from the [canonical Releases page](https://github.com/0FL01/confluence-7.9.0-go-mcp/releases). The GitHub release workflow builds these six standalone binaries with `CGO_ENABLED=0` and publishes `checksums.txt` alongside them:

| Platform | Artifact |
| --- | --- |
| Linux amd64 | `confluence-7.9.0-go-mcp-linux-amd64` |
| Linux arm64 | `confluence-7.9.0-go-mcp-linux-arm64` |
| macOS amd64 | `confluence-7.9.0-go-mcp-macos-amd64` |
| macOS arm64 | `confluence-7.9.0-go-mcp-macos-arm64` |
| Windows amd64 | `confluence-7.9.0-go-mcp-windows-amd64.exe` |
| Windows arm64 | `confluence-7.9.0-go-mcp-windows-arm64.exe` |

On Linux or macOS, make the downloaded file executable and use its absolute path as the MCP command. For example, for a future Linux amd64 release:

```bash
chmod +x confluence-7.9.0-go-mcp-linux-amd64
```

## Configuration

`CONFLUENCE_API_TOKEN` is required. It must be a **personal access token from Confluence Server 7.9.0**, sent as `Authorization: Bearer <token>`. A Confluence Cloud API token is not the authentication method used here.

Set at least one URL variable. The first non-empty value wins in this order:

| Priority | Variable | Purpose |
| --- | --- | --- |
| 1 | `CONFLUENCE_BASE_URL` | Confluence instance URL, including any context path |
| 2 | `CONFLUENCE_API_BASE_PATH` | Alternative URL, which may already end in `/rest/api` |
| 3 | `CONFLUENCE_HOST` | Alternative hostname or URL |

URLs must have a hostname, use `http` or `https`, and contain no query string or fragment. Values without a scheme default to HTTPS. The server preserves the context path and appends `/rest/api` unless the URL path already ends in `/rest/api`.

| Configured value | REST root |
| --- | --- |
| `confluence.example.com` | `https://confluence.example.com/rest/api` |
| `https://confluence.example.com/confluence` | `https://confluence.example.com/confluence/rest/api` |
| `https://confluence.example.com/confluence/rest/api` | `https://confluence.example.com/confluence/rest/api` |

Example environment for running from the repository root:

```bash
export CONFLUENCE_API_TOKEN="your-server-personal-access-token"
export CONFLUENCE_BASE_URL="https://confluence.example.com/confluence"
go run .
```

## Tools

The five tool IDs and their argument names are listed below. Successful calls return the **raw Confluence REST response JSON as MCP text**. Validation and API failures return an MCP tool-error result. Request context is retained, and the HTTP client has a 30-second timeout.

Optional numeric arguments must be finite whole numbers in the REST signed 32-bit integer range. `limit` and `start` accept `0` through `2147483647`; `version` accepts `1` through `2147483647`. Fractional and out-of-range values are rejected. An omitted `limit` defaults to `25`; an explicit `limit: 0` is sent unchanged to Confluence and does not carry a count-only guarantee. Pagination applies only to search and space listing.

### `confluence_get_content`

Retrieve content with `GET /content/{contentId}` relative to the configured REST root.

**Arguments:**
- `contentId` (string, required): Non-empty content ID; IDs containing `/` or `..` are rejected before a request.
- `expand` (string, optional): Comma-separated content expansions, such as `space,version`.

`body.storage` is always included in the expansions, together with any caller-supplied expansions. This request has no pagination parameters.

### `confluence_search_content`

Search with `GET /search` using caller-supplied CQL, passed through unchanged.

**Arguments:**
- `cql` (string, required): Non-empty Confluence Query Language search string.
- `limit` (number, optional): Page size; defaults to `25` when omitted.
- `start` (number, optional): Zero-based starting index; omitted values use Confluence's default.
- `expand` (string, optional): Comma-separated native search expansions, such as `content.body.storage,content.space`.

The response retains Confluence's paginated `SearchResult` shape: content is nested under `results[].content`, and space results are nested under `results[].space`. It is not flattened into a content array. Use search-specific expansion paths to read nested fields, for example:

```json
{
  "cql": "type=page",
  "limit": 25,
  "start": 0,
  "expand": "content.body.storage,content.space"
}
```

### `confluence_create_content`

Create a page or blog post with `POST /content`.

**Arguments:**
- `title` (string, required): Non-empty title.
- `spaceKey` (string, required): Non-empty key of the destination space.
- `content` (string, required): Non-empty body in Confluence **storage** format, for example `<p>Hello</p>`; not Markdown.
- `type` (string, optional): `page` or `blogpost`; omitted or empty values default to `page`.
- `parentId` (string, optional): Parent content ID, used to set the ancestors for a child page. Omit for standalone content.

### `confluence_update_content`

Fetch current content with `GET /content/{contentId}?expand=body.storage,version,space`, then update with `PUT /content/{contentId}`. Neither request uses pagination.

**Arguments:**
- `contentId` (string, required): Non-empty content ID; IDs containing `/` or `..` are rejected before a request.
- `version` (number, optional): Target version, from `1` through `2147483647`; defaults to the current version plus one.
- `title` (string, optional): Replacement title; omitted or empty values preserve the fetched title.
- `content` (string, optional): Replacement body in storage format; omitted or empty values preserve the fetched body.
- `versionComment` (string, optional): Comment for the new version.

The fetched type and space are preserved. Fields being reused must be complete; otherwise the update fails before the PUT. For a preserved page or blog post body, `body.storage.value` must be present and non-null. A fetched empty string is valid and is preserved. Legitimately bodyless content types do not require a storage body.

Without an explicit `version`, the current version must be available and incrementing it must remain within the allowed range. An explicit target version is sent as supplied, even if the fetched version is absent; it is not replaced by the default or forced to match the fetched snapshot. API conflicts are returned as tool errors, with no automatic retry or rebase.

### `confluence_list_spaces`

List or search spaces using `GET /search`, preserving the native paginated search response with space data under `results[].space`.

**Arguments:**
- `searchText` (string, optional): Search space titles with CQL `title ~ "..."`. Omitted or empty values use `type=space`.
- `limit` (number, optional): Page size; defaults to `25` when omitted.
- `start` (number, optional): Zero-based starting index; omitted values use Confluence's default.
- `expand` (string, optional): Comma-separated native search expansions, such as `space.homepage`.

This tool returns one result page, including when `searchText` is omitted. Advance `start` to retrieve further pages. Searches match titles, not descriptions. Backslashes and quotes are escaped for containment in the CQL string; the `~` operator's Lucene search syntax retains its meaning.

```json
{
  "searchText": "Documentation",
  "limit": 25,
  "start": 0,
  "expand": "space.homepage"
}
```

## Development and Builds

Use Go 1.25.5 and run checks from the repository root:

```bash
go vet ./...
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
go build -v ./...
```

Configuration tests isolate `CONFLUENCE_API_TOKEN`, `CONFLUENCE_BASE_URL`, `CONFLUENCE_API_BASE_PATH`, and `CONFLUENCE_HOST`; no production credentials are used. The automated tests use local HTTP fixtures to verify client contracts; they do not establish compatibility against a live Confluence installation.

Local builds use `go build`. GitHub Actions runs tests and lint plus a six-target build matrix; the release workflow publishes the six platform builds for version tags matching `v*.*.*`.

## Project Structure

```text
.
├── main.go                       # Server, tool handlers, and REST client
├── main_test.go                  # Client and handler contract tests
├── go.mod                        # Go module and dependency versions
├── go.sum                        # Dependency checksums
├── .github/workflows/ci.yml      # Test, lint, and six-target build checks
├── .github/workflows/release.yml # Cross-platform release builds
├── LICENSE                       # MIT license and original copyright
└── README.md                     # This file
```

## API Reference

- [Confluence Server REST API 7.9.0](https://docs.atlassian.com/ConfluenceServer/rest/7.9.0/) — pinned REST specification for the target version.
- [Confluence 7.9 release notes](https://confluence.atlassian.com/doc/confluence-7-9-release-notes-1026537698.html) — Server personal access token support.
- [CQL field reference](https://developer.atlassian.com/server/confluence/cql-field-reference/).
- [Advanced searching using CQL](https://developer.atlassian.com/server/confluence/advanced-searching-using-cql/).

## License and Acknowledgments

This project is licensed under the [MIT License](LICENSE). The original license and copyright are preserved.

Historical attribution:
- Upstream Go implementation: [Anudeep Dhavaleswarapu's atlassian-confluence-dc-go-mcp](https://github.com/anudeepd/atlassian-confluence-dc-go-mcp).
- Original TypeScript implementation that inspired the Go rewrite: [b1ff/atlassian-dc-mcp](https://github.com/b1ff/atlassian-dc-mcp).
- MCP Go SDK: [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).
- [Model Context Protocol](https://modelcontextprotocol.io/).

For issues or contributions, use the [canonical GitHub repository](https://github.com/0FL01/confluence-7.9.0-go-mcp).

### Upstream release history

These versions belong to the original upstream module, not to this repository's canonical module or releases. Historical older-Go workarounds are not supported here; use Go 1.25.5.

- **v1.0.2:** documented building with older Go versions.
- **v1.0.1 (deprecated upstream):** fixed the upstream module path; later deprecated for configuration issues.
- **v1.0.0 (deprecated upstream):** initial Go rewrite, core Confluence tools, multi-platform builds and MCP support; later deprecated for module configuration issues.
