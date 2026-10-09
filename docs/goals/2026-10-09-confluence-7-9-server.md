# Goal: Confluence 7.9.0 Server-only MCP

Status: active
Source: audited plan approved by the user, followed by “делай копию плана в цель и итеративно реализовать и коммит пуш”; latest instruction removes build.sh and uses go build / GitHub CI/CD.
Last updated: 2026-10-09

## Objective
Implement and publish the audited Server 7.9.0 refactor, preserving the five MCP tool IDs and existing response contracts. Remove build.sh; local builds use Go and cross-platform releases use GitHub Actions.

## Execution Directive
Complete the frozen Required Outcomes using the listed Change Envelope and Primary Evidence. Work on the smallest unresolved outcome. Do not add requirements from reviews, tests, tools, speculative risks, or optional source text. Finish when every required outcome is resolved and affected constraints remain satisfied.

## Frozen Contract / Plan

### R1: Canonical identity and distribution
- Source: audited plan A; latest user build.sh instruction.
- Acceptance: module github.com/0FL01/confluence-7.9.0-go-mcp; MCP name and six release artifact prefixes confluence-7.9.0-go-mcp; no build.sh or instructions requiring it; inherited upstream retractions removed; active metadata/docs target only Server 7.9.0 selfhosted. Preserve LICENSE and historical attribution. README does not claim an existing canonical release.
- Plan: update go.mod, setupServer, GitHub release outputs/uploads and README together; delete build.sh; refresh stale AGENTS guidance. Keep application version and dependencies unchanged. No redundant release mkdir (Go creates output directories).
- Primary evidence: metadata tests, go build, release workflow review and repository diff.
- Status: verified
- Evidence: canonical module/metadata/docs and six release output/upload names reviewed; TestSetupServer passed under Go 1.25.5; build.sh deleted, no replacement; CI build matrix covers all six targets.

### R2: URL and REST request contracts
- Source: audited plan B.
- Acceptance: retain URL precedence BASE_URL > API_BASE_PATH > HOST, HTTPS default and context paths; require exact HTTP/HTTPS and hostname, forbid query/fragment, recognize REST root by /rest/api suffix. Only search/list have pagination; get merges body.storage; update GET expands body.storage,version,space. Preserve /search and raw SearchResult response JSON. Space search is title-based and paginated; escape backslashes before quotes for CQL containment without changing Lucene semantics or raw user CQL.
- Plan: narrowly change loadConfig, pagination construction, get/update queries and generated space CQL; align schemas and README native expansion examples.
- Primary evidence: exact request/query and configuration contract tests.
- Status: verified
- Evidence: Go 1.25.5 focused suite passed configuration/precedence, exact content/search query and raw SearchResult response assertions, including zero-limit space search and mixed backslash/quote escaping.

### R3: Validation and safe update preservation
- Source: audited plan C.
- Acceptance: optional MCP numbers are finite integral REST int32 values; start >= 0, version >= 1; limit 0 remains transmitted, without promising count-only behavior or replacing it with default 25. Create accepts page/blogpost and omitted/empty type defaults to page; schema agrees. Update preserves fetched type/space and omitted/empty title/content; distinguishes absent/null reused storage.value from a present empty string; malformed needed fields stop before PUT. Validate only fields being reused, do not require storage for legitimately bodyless types. Default version is current + 1 with overflow checks; preserve explicit target version even without current version. No retry/rebase/overwrite policy.
- Plan: small numeric helper and response-presence handling, not a generic validation framework. Explicit version remains caller-controlled, not universal fetched-snapshot CAS protection.
- Primary evidence: validation and update payload/sequence tests, including no PUT on incomplete GET and exactly one PUT on 409.
- Status: verified
- Evidence: focused validation/create/update tests passed; invalid input made no HTTP calls; incomplete preserved fields made no PUT; valid empty body preserved; explicit version without current version and one PUT on 409 verified.

### R4: Automated verification
- Source: audited plan D and AGENTS verification commands, with script gate superseded by latest instruction.
- Acceptance: tests isolate all four config variables; deterministic transport errors; complete independent JSON update fixtures; exact request/response/error assertions; child page and standalone blogpost fixtures; five-tool/schema/initialize assertions. Keep existing JSON/read/run/error coverage. No callback t.Fatal or race-unsafe request capture. Go 1.25.5 vet, race/coverage tests and build pass; existing GitHub lint/build checks remain enabled.
- Plan: test changed contracts and a small transport invariant set (Bearer/JSON, context, timeout configuration, body closure). No exhaustive transport matrix, real timeout waits or coverage percentage target. Cross-platform builds belong to GitHub CI/CD, not a replacement script.
- Primary evidence: go vet ./...; go test -v -race -coverprofile=coverage.out -covermode=atomic ./...; go build -v ./...; GitHub Actions checks after push.
- Status: in_progress
- Evidence: Go 1.25.5 go vet, full race/atomic coverage suite (98.1%) and go build passed; go mod verify passed. GitHub jobs will be inspected after push.

### R5: Commit and push
- Source: latest explicit user instruction.
- Acceptance: reviewed intended changes committed and pushed to origin/main, without generated binaries/coverage or credentials.
- Primary evidence: git status/diff/log review, commit hash and successful push; matching remote branch.
- Status: in_progress
- Evidence: intended source/docs/workflow diff and recent Git log reviewed; generated artifacts will be removed before staging. Commit/push pending.

### Constraints
- Five confluence_* IDs and argument names remain; success is raw REST JSON as MCP text; failures are tool-error + nil Go error; stdout stays MCP-only.
- PAT/Bearer, request context, 30-second timeout and response-body ownership/closure remain.
- Storage representation, URL aliases and invalid content-ID protection remain.
- Keep one main.go package, current Go/SDK/dependencies and application version; no edition detector, endpoint fallback, new auth, response wrapper or retry framework.

### Non-goals / compatibility evidence boundary
- Automated tests verify the client contract, not a live installation. No Confluence instance, PAT/test-space access or write approval was provided; live smoke is not a prerequisite for automated refactor completion and no live-compatibility claim will be made.
- Optional later smoke requires exact Server edition 7.9.0: PAT, all five tools, page/child/blogpost, storage read-back, native search expansions, default/explicit updates, bounded indexing wait and approved cleanup. It does not add application retry logic or server provisioning work.
- No dependency upgrades, CI modernization, release publication or unrelated hardening.

## Change Envelope
- main.go, main_test.go, go.mod, README.md, AGENTS.md, .github/workflows/ci.yml and release.yml; delete build.sh; this goal document.
- Tests may use repository-local fixtures; no secrets, generated build/coverage artifacts or external services are committed.

## Primary specifications
- https://docs.atlassian.com/ConfluenceServer/rest/7.9.0/
- https://confluence.atlassian.com/doc/confluence-7-9-release-notes-1026537698.html (Server PAT support)
- https://developer.atlassian.com/server/confluence/cql-field-reference/
- https://go.dev/ref/mod#go-mod-file-retract

## Current Checkpoint
- Closes: R4–R5.
- Next: remove generated artifacts, commit/push the reviewed implementation, inspect GitHub checks, then record closure.
- Expected evidence: green local gates and GitHub six-target build/test/lint jobs.

## Current State
- Initial branch main tracked origin/main and was clean.
- Default Go is 1.26.8; Go 1.25.5 installed and selected with GOTOOLCHAIN=go1.25.5 for gates; go mod verify passed.
- R1–R3 implemented; focused and full local gates passed. Publication and GitHub checks remain.
- Live smoke: not performed; qualifying server/access not supplied.
- Blocker: none for implementation or automated verification.

## Material Decisions
- 2026-10-09: latest instruction supersedes all script/cross-build-script plan items. Delete build.sh; use go build locally and GitHub CI/CD for platform builds.

## Checkpoint History
- 2026-10-09: contract frozen before implementation; initial clean Git state and workflows inspected.
- 2026-10-09: R1–R3 completed; focused Go 1.25.5 handler/config/schema/transport tests passed. Local script removed; GitHub matrix replaces the old cross-build gate. Next: full gates and publication.
- 2026-10-09: full Go 1.25.5 vet/race/build gates passed, coverage 98.1%. GitHub CLI installed outside the repository; no API login is configured, so public Actions evidence may require unauthenticated read-only access. Next: publish and inspect checks.

## Completion
- Implementation and local verification complete; publication and GitHub evidence pending.
