# Confluence MCP

Give your AI agent five tools to read, search, create, and update Confluence content and find spaces. A single Go binary runs locally over MCP stdio and calls your Confluence REST API.

**Supported version: Confluence Server 7.9.0, self-hosted only.** Data Center and Cloud are not compatibility targets.

| Tool | What it does |
| --- | --- |
| `confluence_get_content` | Retrieve content by ID, including its storage body |
| `confluence_search_content` | Search content and spaces using CQL |
| `confluence_create_content` | Create a page or blog post with a storage-format body |
| `confluence_update_content` | Update content while preserving unchanged fields |
| `confluence_list_spaces` | List spaces or search their titles |

## Quick start

### 1. Prepare a private `.env`

Use a **personal access token from Confluence Server 7.9.0**. The server sends it as `Authorization: Bearer <token>`.

Create `.env` in a private configuration directory, for example `/absolute/path/to/private/confluence-config/.env`:

```dotenv
CONFLUENCE_BASE_URL='https://confluence.example.invalid/confluence'
CONFLUENCE_API_TOKEN='replace-with-your-server-personal-access-token'
```

Replace the example URL and token. Include your Confluence context path if applicable. Keep the file out of version control and restrict access, for example on Linux or macOS:

```bash
chmod 600 /absolute/path/to/private/confluence-config/.env
```

The server reads `.env` from its **working directory**. Inherited environment variables override the same keys in the file, **including empty values**; an empty inherited token causes startup to fail.

### 2. Install `confluence-mcp`

No release has been published under the [canonical repository](https://github.com/0FL01/confluence-7.9.0-go-mcp) yet. Build from source with **Go 1.25.5**, as specified by `go.mod` and CI:

```bash
git clone https://github.com/0FL01/confluence-7.9.0-go-mcp.git
cd confluence-7.9.0-go-mcp
go build -o confluence-mcp .
```

On Windows, use `go build -o confluence-mcp.exe .`. On Linux or macOS, you can install the binary into `~/.local/bin`:

```bash
mkdir -p "$HOME/.local/bin"
install -m 755 confluence-mcp "$HOME/.local/bin/confluence-mcp"
```

After the first manual release, you can use the prebuilt archives described below.

### 3. Configure OpenCode

Merge this into your project or global `opencode.jsonc`, preserving other settings:

```json
{
  "mcp": {
    "confluence": {
      "type": "local",
      "command": ["/absolute/path/confluence-mcp"],
      "cwd": "/absolute/path/to/private/confluence-config",
      "timeout": 35000,
      "enabled": true
    }
  }
}
```

Replace both paths with real absolute paths; `cwd` must contain your `.env`. For example, the command can be `/home/your-user/.local/bin/confluence-mcp` after installation. The server loads the file itself. Keep credentials in the private `.env`; inherited `CONFLUENCE_*` values must be unset if the file should supply those keys.

### 4. Verify startup and read content

Restart OpenCode after configuring the server, then check it from the same workspace:

```bash
opencode mcp list
```

Before starting MCP stdio, the server checks `/rest/api/user/current` and requires HTTP `200` with `type: "known"`. Once connected, ask OpenCode to call `confluence_get_content` with the `contentId` of a page your account can access. This also verifies that account's permission to read the page.

If startup fails, check `cwd`, `.env` readability and syntax, inherited environment overrides, the Confluence URL, and the PAT. Diagnostics go to stderr; stdout is reserved for MCP protocol traffic. See the configuration details below for the full startup contract.

<details>
<summary>Prebuilt archives and checksum verification</summary>

Once a release is available, download the archive for your platform and `checksums.txt` from the [canonical Releases page](https://github.com/0FL01/confluence-7.9.0-go-mcp/releases).

| Platform | Archive |
| --- | --- |
| Linux amd64 | `confluence-mcp_linux_amd64.tar.gz` |
| Linux arm64 | `confluence-mcp_linux_arm64.tar.gz` |
| macOS amd64 | `confluence-mcp_darwin_amd64.tar.gz` |
| macOS arm64 | `confluence-mcp_darwin_arm64.tar.gz` |
| Windows amd64 | `confluence-mcp_windows_amd64.zip` |
| Windows arm64 | `confluence-mcp_windows_arm64.zip` |

All six archives contain the native `confluence-mcp` binary (`confluence-mcp.exe` on Windows), `README.md`, and `LICENSE`. Binaries are built with `CGO_ENABLED=0`; `checksums.txt` contains SHA-256 hashes.

Verify the selected archive before extracting it. For Linux amd64:

```bash
sha256sum confluence-mcp_linux_amd64.tar.gz
```

Compare the printed hash with the entry for **that exact filename** in the downloaded `checksums.txt`. On macOS, use `shasum -a 256` with your selected archive instead. After the hash matches, extract and install; for Linux amd64:

```bash
tar -xzf confluence-mcp_linux_amd64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 confluence-mcp "$HOME/.local/bin/confluence-mcp"
```

On Windows, compute the SHA-256 hash in PowerShell, compare it with the matching `checksums.txt` entry, then extract the ZIP. For Windows amd64:

```powershell
Get-FileHash .\confluence-mcp_windows_amd64.zip -Algorithm SHA256
```

After the hash matches:

```powershell
Expand-Archive .\confluence-mcp_windows_amd64.zip -DestinationPath .\confluence-mcp
```

Use the extracted binary's absolute path as the MCP command.

</details>

<details>
<summary>Configuration, authentication, and other MCP clients</summary>

### Configuration

`CONFLUENCE_API_TOKEN` is required. It must be a **personal access token from Confluence Server 7.9.0**, sent as `Authorization: Bearer <token>`. A Confluence Cloud API token is not the authentication method used here.

### `.env` and inherited environment

The server reads only `.env` in its current working directory using `godotenv.Read` from `github.com/joho/godotenv` v1.5.1. It does not search parent directories, the executable directory, or `.env.local`. A missing `.env` permits environment-only configuration; a present but unreadable or malformed file fails startup safely.

File values and the inherited process environment are merged without mutating the process environment. An inherited environment variable overrides the **same key** from `.env`, including when its value is empty. An empty inherited `CONFLUENCE_API_TOKEN`, for example, overrides a file token and fails token validation.

Set at least one URL variable. After merging, the first non-empty value wins in this order:

| Priority | Variable | Purpose |
| --- | --- | --- |
| 1 | `CONFLUENCE_BASE_URL` | Confluence instance URL, including any context path |
| 2 | `CONFLUENCE_API_BASE_PATH` | Alternative URL, which may already end in `/rest/api` |
| 3 | `CONFLUENCE_HOST` | Alternative hostname or URL |

Prefer setting only `CONFLUENCE_BASE_URL`. URL aliases retain their priority after merging: a `CONFLUENCE_BASE_URL` from `.env` takes priority over an inherited `CONFLUENCE_HOST`, because those are different keys.

URLs must have a hostname, use `http` or `https`, and contain no query string or fragment. Values without a scheme default to HTTPS. The server preserves the context path and appends `/rest/api` unless the URL path already ends in `/rest/api`.

| Configured value | REST root |
| --- | --- |
| `confluence.example.com` | `https://confluence.example.com/rest/api` |
| `https://confluence.example.com/confluence` | `https://confluence.example.com/confluence/rest/api` |
| `https://confluence.example.com/confluence/rest/api` | `https://confluence.example.com/confluence/rest/api` |

As an optional alternative to `.env`, set the inherited environment when running from the repository root:

```bash
export CONFLUENCE_API_TOKEN='replace-with-your-server-personal-access-token'
export CONFLUENCE_BASE_URL='https://confluence.example.invalid/confluence'
go run .
```

Clients that use the `mcpServers` configuration format, such as Claude Desktop, can also supply environment variables directly. This client-specific format differs from the OpenCode example above:

```json
{
  "mcpServers": {
    "confluence": {
      "command": "/absolute/path/confluence-mcp",
      "env": {
        "CONFLUENCE_API_TOKEN": "replace-with-your-server-personal-access-token",
        "CONFLUENCE_BASE_URL": "https://confluence.example.invalid/confluence"
      }
    }
  }
}
```

### Authenticated startup

After configuration and PAT validation, the server sends `GET /rest/api/user/current` under the configured Confluence context path before starting MCP stdio. It requires HTTP `200` and a JSON user object with `type: "known"`. This request uses the client's 30-second timeout and is not retried.

Invalid configuration, an unreadable or malformed `.env`, anonymous users, HTML or invalid JSON responses, non-200 API responses (including `400`, `401`, and `403`), and network failures cause startup to exit with status `1`. Startup diagnostics go to stderr, omit secrets, and never include raw server response bodies; stdout remains reserved for MCP protocol traffic.

This check proves authentication as a known user. Access to individual content and spaces still depends on that user's permissions.

</details>

<details>
<summary>Tool arguments and REST contracts</summary>

### Tool contract

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

</details>

<details>
<summary>Development, CI, and manual releases</summary>

### Development

The executable is `confluence-mcp` (`confluence-mcp.exe` on Windows). The Go module is [`github.com/0FL01/confluence-7.9.0-go-mcp`](https://github.com/0FL01/confluence-7.9.0-go-mcp); the MCP server name remains `confluence-7.9.0-go-mcp`, with application version `1.0.0`. Dependencies are pinned in `go.mod`, including `github.com/mark3labs/mcp-go` v0.43.2 and `github.com/joho/godotenv` v1.5.1.

Use Go 1.25.5 and run checks from the repository root:

```bash
go vet ./...
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
go build -v -o confluence-mcp .
```

Configuration tests isolate `CONFLUENCE_API_TOKEN`, `CONFLUENCE_BASE_URL`, `CONFLUENCE_API_BASE_PATH`, and `CONFLUENCE_HOST`; no production credentials are used. The automated tests use local HTTP fixtures to verify client contracts; they do not establish compatibility against a live Confluence installation.

For local development, run `go run .` from the repository root with a `.env` there or with the environment variables set.

### CI and release builds

Local builds use `go build`. GitHub Actions keeps the Test and Lint gates, validates `.goreleaser.yaml` with `goreleaser check`, and builds all six platforms with `goreleaser release --snapshot --clean`. Snapshot builds do not publish releases.

GoReleaser v2 builds the root package for Linux, macOS, and Windows on amd64 and arm64, with `CGO_ENABLED=0` and `-s -w`. It packages the archives listed above and generates `checksums.txt`.

### Publish a release manually

The Release workflow runs **only through `workflow_dispatch`**. After pushing the intended changes to `main`:

1. Open [Actions → Release](https://github.com/0FL01/confluence-7.9.0-go-mcp/actions/workflows/release.yml).
2. Select **Run workflow** and choose branch **main**.
3. Enter the required **version**, for example `v1.0.0`.
4. Select **Run workflow** to publish the release.

The version must be stable `vMAJOR.MINOR.PATCH`, with no leading zeros (except the number `0` itself). An existing version fails. The workflow checks the selected `main` commit before creating and pushing a new tag for that SHA, then runs `goreleaser release --clean`. A tag push alone does not trigger publishing.

If a failure occurs after the tag is pushed, a tag or draft release may remain. Resolve that state manually; the workflow does not force-update tags or automatically clean up a failed release.

</details>

<details>
<summary>Confluence API references</summary>

- [Confluence Server REST API 7.9.0](https://docs.atlassian.com/ConfluenceServer/rest/7.9.0/) — pinned REST specification for the target version.
- [Confluence 7.9 release notes](https://confluence.atlassian.com/doc/confluence-7-9-release-notes-1026537698.html) — Server personal access token support.
- [CQL field reference](https://developer.atlassian.com/server/confluence/cql-field-reference/).
- [Advanced searching using CQL](https://developer.atlassian.com/server/confluence/advanced-searching-using-cql/).

</details>

## License

This project is licensed under the [MIT License](LICENSE). The original license and copyright are preserved.

<details>
<summary>Attribution and upstream release history</summary>

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

</details>
