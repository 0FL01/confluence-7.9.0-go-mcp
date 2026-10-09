# Goal: dotenv credentials and fail-fast bootstrap

Status: complete
Source: approved dotenv/bootstrap plan; user: “реализовать и коммит пуш билд”, install at ~/.local/bin/confluence-mcp and rename the executable to confluence-mcp.
Last updated: 2026-10-09

## Objective
Load private credentials from the process working directory's .env, verify Confluence authentication before MCP stdio starts, then build/install confluence-mcp and publish the reviewed implementation.

## Execution Directive
Complete the frozen Required Outcomes using the listed Change Envelope and Primary Evidence. Work on the smallest unresolved outcome. Do not add requirements from reviews, tests, tools, speculative risks, or optional source text. Finish when every required outcome is resolved and affected constraints remain satisfied.

## Frozen Contract
- R1: dotenv configuration.
  - Source: approved plan steps 1–2.
  - Acceptance: godotenv v1.5.1 reads only cwd/.env without mutating process ENV; existing ENV wins per key even when empty. Missing file permits ENV-only startup; unreadable/malformed file fails with secret-free diagnostics. Preserve URL alias order and normalization after merging sources.
  - Primary evidence: isolated configuration tests using only temporary directories and synthetic credentials.
  - Status: verified
  - Evidence: TestLoadConfigDotenv and existing configuration tests pass; file/ENV/empty-value/alias cases, unrelated ENV preservation, invalid/unreadable files and cwd-only lookup covered.
- R2: fail-fast authenticated bootstrap.
  - Source: user requested failed startup on absent connection / HTTP 400/401/403 etc.; approved plan step 3.
  - Acceptance: GET /user/current through existing client transport before setupServer/serve; require HTTP 200 and JSON type known. Anonymous, malformed/HTML response, non-200 status and transport failures stop startup. Show actual HTTP status or network cause, never credentials/raw response bodies; preserve timeout/context/body closure. No retry or offline fallback.
  - Primary evidence: client and run tests proving request/auth, response closure, and no serve call on failure; installed-binary smoke proving exit/stderr/stdout behavior.
  - Status: verified
  - Evidence: TestCheckConnection, TestRun and TestRunDotenvBootstrap pass. TestBinaryBootstrap passes against ~/.local/bin/confluence-mcp: authenticated initialize, HTTP 401, anonymous, malformed dotenv and offline cases; failed startup exits 1 without stdout or secret markers.
- R3: executable name and private configuration docs.
  - Source: latest binary rename instruction and approved plan step 5.
  - Acceptance: documented local/CI output and six release artifact prefixes are confluence-mcp; .env files ignored; README shows OpenCode cwd and 35000-ms initialization timeout without credentials in its config. AGENTS records new configuration/bootstrap invariants.
  - Primary evidence: reviewed paths/docs/workflows, workflow lint and binary metadata.
  - Status: verified
  - Evidence: six release build/upload names match README; CI explicitly names both executable variants; actionlint 1.7.12 passes both workflows. git check-ignore confirms .env/.env.* ignored at root and nested paths. go version -m confirms unchanged module and godotenv v1.5.1; MCP smoke confirms unchanged server identity.
- R4: verified build and installation.
  - Source: latest build/install instruction and approved plan verification.
  - Acceptance: Go 1.25.5 vet, full race/coverage suite, build and lint pass; executable installed at ~/.local/bin/confluence-mcp with matching build checksum, then smoke-tested against a local mock (no production access).
  - Primary evidence: gate output, executable/checksum comparison and controlled binary smoke.
  - Status: verified
  - Evidence: Go 1.25.5 mod verify, vet, full race/atomic coverage (98.3%) and CGO_ENABLED=0 build pass; golangci-lint 2.14.0 reports 0 issues. Rebuilt from clean published commit 9adfe5682dd2e70b678ac0ee60e4fbb3d063e795 and installed with mode 755; build/install SHA-256 both 501acb32e184596bde1ea50856cfc3b319a455d1abeacd65d91b1bb923785b35. All five installed-binary smoke cases pass with -count=1 (no test-cache reuse); go version -m confirms the commit and vcs.modified=false.
- R5: commit and push.
  - Source: latest explicit instruction.
  - Acceptance: reviewed intended changes committed/pushed to origin/main; no secrets or generated artifacts committed.
  - Primary evidence: commit hashes, successful push and clean matching remote branch.
  - Status: verified
  - Evidence: reviewed implementation commit 9adfe5682dd2e70b678ac0ee60e4fbb3d063e795 pushed successfully to origin/main; git ls-remote confirmed matching main and git status was clean. Only the ten approved source/docs/workflow files were committed; no generated artifacts or private credentials.

### Constraints and non-goals
- ONLY Confluence 7.9.0 Server selfhosted. Preserve five tool IDs/schemas, raw JSON/error contracts, Bearer/context/30-second timeout, URL aliases and update preservation.
- The rename is executable-only: module/repository path and advertised MCP server identity/version remain unchanged. Use go build -o explicitly; no build.sh.
- Private .env is user-owned; never create/read production credentials. Tests must isolate both all four ENV keys and cwd, without t.Parallel.
- CI/CD redesign and full Jira-style README rewrite remain paused. Only update current build/artifact names; no release/tag creation, new flags, hot reload or retry framework.
- Live Confluence proof and GitHub Actions runtime are not prerequisites for this local build/install objective; old Actions authorization blocker is recorded in the prior separate goal.

## Change Envelope
- main.go, main_test.go, go.mod/go.sum, .gitignore, README.md, AGENTS.md, existing CI/release artifact paths, this document.
- One explicitly approved parser dependency: github.com/joho/godotenv v1.5.1; no other dependency/version changes.
- Generated coverage/build/smoke artifacts stay in $TMPDIR; user-approved installed binary only at ~/.local/bin/confluence-mcp.

## Current Checkpoint / State
- R1–R5 verified. Closure passed; no remaining implementation checkpoint.
- Initial Git state: clean main...origin/main at 0d6df0e. Native Go 1.25.5 linux/amd64 binary is installed at ~/.local/bin/confluence-mcp.
- Blocker: none. No live credentials/server used; automated and installed-binary evidence comes from local mocks.

## Evidence Sources
- https://docs.atlassian.com/ConfluenceServer/rest/7.9.0/#api/user-getCurrent
- https://docs.atlassian.com/ConfluenceServer/javadoc/7.9.0/constant-values.html (known / anonymous person types)
- https://raw.githubusercontent.com/joho/godotenv/v1.5.1/godotenv.go
- https://opencode.ai/docs/mcp-servers/ (cwd, command array, initialization timeout)

## Checkpoint History
- 2026-10-09: contract frozen before implementation; executable-only rename interpretation recorded, current branch/toolchain/destination verified.
- 2026-10-09: R1–R4 pass. One lint failure (ST1005, capitalized error prefix) corrected without suppression; final lint/tests/build pass. URL parse errors also omit input to prevent dotenv credential disclosure. Full Go source/docs/workflow diff reviewed; generated artifacts remain outside the worktree.
- 2026-10-09: R5 verified by successful push and matching remote SHA. Rebuilt/installed from clean published source and reran all installed-binary smoke cases without cache reuse. Closure confirms all outcomes, preserved contracts and approved diff scope.

## Completion
- All R1–R5 verified; goal complete with no blocker.
- Go 1.25.5 configuration/client/run tests, vet, full race/atomic coverage, build, golangci-lint and actionlint pass. Installed executable: ~/.local/bin/confluence-mcp, clean source revision 9adfe5682dd2e70b678ac0ee60e4fbb3d063e795; authenticated MCP and four failure smoke cases pass.
- Closure: five tool contracts, MCP identity, transport/storage/update invariants preserved; only approved files/dependency changed; generated artifacts outside Git and no private credentials accessed. CI/CD redesign remains paused; no live Confluence or GitHub Actions execution claim.
