# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`ssh-mcp-light` is a stateless MCP (Model Context Protocol) stdio server, in Go, that gives an LLM agent
controlled access to a small set of pre-declared VMs over SSH. Every tool call resolves its target host
and paths from two TOML config files on disk — there is no free-form host/port/user parameter anywhere in
the tool surface. There is no separate spec document — the code and its comments are the source of truth.
Comments explain *why* something is done a particular way, not what the following line already says — keep
that balance when adding to them.

## Commands

```sh
go build -o ssh-mcp-light ./cmd/ssh-mcp-light   # build
go vet ./...                                     # vet
go test ./...                                    # full test suite (no network beyond localhost, no real VM)
go test ./internal/tools/... -run TestPush_Success -v   # a single test
golangci-lint run ./...                          # lint gate (errcheck, gosimple, govet, ineffassign,
                                                  # staticcheck, unused, gofmt, goimports — see .golangci.yml)
```

`go test ./internal/sshlayer/...` takes ~12s (real timeout/truncation/connect-timeout tests against an
in-process fixture); everything else is near-instant. `.gitlab-ci.yml` runs the same lint/test/build
commands as separate stages on every push.

## Architecture

Five MCP tools (`host_list`, `path`, `exec`, `push`, `sync`) implemented in `internal/tools`, wired onto
an SDK server in `internal/mcpserver`, started by `cmd/ssh-mcp-light/main.go`. `main.go` splits config
resolution (`loadConfigs`) from actually running the server so startup validation is testable without
blocking on stdio.

Package dependency direction (no cycles): `config`, `pathsafe`, `ignore`, `sshlayer`, `errcodes` are leaf
packages; `syncengine` depends on `pathsafe` + `sshlayer` + `errcodes`; `tools` depends on all of the above
plus `syncengine`; `mcpserver` and `cmd` sit on top.

- **`internal/config`** — parses and validates `vm.toml` and the project config file. Resolves
  `<project-root>` (`ProjectRoot`) and, separately, its symlink-resolved canonical form
  (`ProjectRootCanonical`) once at startup. The canonical form exists *only* for confinement comparisons —
  never shown to a caller — so that a symlinked ancestor of the project root doesn't make every file look
  like it's escaping.

- **`internal/pathsafe`** — the traversal checks. Local checks (`CheckLocalLexical`, `CheckLocalFile`)
  compare against the project root; remote checks (`CheckRemoteLexical`, `CheckRemoteReal`) compare against
  a *canonical remote base* that `internal/tools` resolves fresh via one SFTP real-path call per `push`/
  `sync` call (see `ensureRemoteBase` in `internal/tools/remotebase.go`) — never against the raw, unresolved
  remote base. `exec` never touches this package: it is intentionally unconfined. If you're ever tempted to
  add a path check to `exec`, don't — that's deliberate, not an oversight.

- **`internal/ignore`** — a self-contained gitignore-compatible matcher (glob, `**`, anchoring, negation,
  nested-`.gitignore` precedence). Deliberately hand-rolled instead of pulling in `go-git` for one
  subpackage (see the git log for why). `Matcher.Ignored` checks every ancestor directory's own match state
  before the candidate path itself, so a directory-matching pattern excludes everything under it even when
  a path is tested independently of a top-down walk (`sync`'s remote deletion-candidate check isn't a
  walk).

- **`internal/sshlayer`** — the only package that imports `golang.org/x/crypto/ssh` and `github.com/pkg/
  sftp`. Defines the `Runner`/`FileTransfer`/`VMConnector`/`HostKeyPolicy` interfaces everything else
  depends on. `Connector.Connect` opens one fresh SSH+SFTP connection per call (not shared/pooled across
  calls) and returns a close func; `AcceptAllHostKeys` is the whole host-key-verification story for this
  version — it's the single seam a future trust-on-first-use/pinning implementation would replace. `exec.go`
  builds the one command string SSH's exec channel carries by POSIX-single-quoting every argv element
  (`BuildCommand`) — there is no native argv exec channel in the SSH protocol, so this quoting is what makes
  `exec` behave like an argv call despite the mandatory shell hop. `sftp.go`'s `Upload` writes to a
  `.{basename}.{hex}.tmp` temp file and renames into place (posix-rename extension first, falling back to
  plain rename / remove-then-rename) so a reader never observes a partially-written file.

- **`internal/syncengine`** — pure planning (`BuildPlan`) and execution (`Apply`) of the sync algorithm,
  given an already-fetched remote file listing (no live connection needed to unit-test it). A remote-only
  path that `include`/`ignore` would exclude if it existed locally is *protected* from deletion, not swept
  — reported in `ProtectedByIgnore`, not `ToDelete`.

- **`internal/errcodes`** — the `E_*` error code constants plus `Classify(err)`, which maps a
  `pathsafe`/`sshlayer` error to its code by type/sentinel. This is the one place that mapping happens;
  both `syncengine` and `tools` call into it rather than duplicating the switch.

- **`internal/tools`** — one file per tool, each registered via the MCP SDK's *raw* `Server.AddTool(tool,
  ToolHandler)` rather than the generic `mcp.AddTool[In, Out]`. This is deliberate: a raw `ToolHandler`
  that returns a Go `error` is treated by the SDK as a *protocol-level* JSON-RPC error, and the generic
  path's automatic output-schema validation would reject an error-shaped payload against a tool's success
  schema anyway. Every handler here does its own JSON unmarshal/validation and always returns
  `(result, nil)`, building `CallToolResult{IsError, StructuredContent, Content}` by hand via `success()`/
  `failure()` in `envelope.go` — so a tool-level failure is always `isError:true` with a structured `{error:
  {...}}` payload, never a JSON-RPC error. `types.go` defines `ResolvedTarget`, echoed on every response
  (success or failure) so a caller can see exactly which VM/path an operation targeted or attempted.

- **`internal/sshtest`** — an in-process SSH+SFTP server fixture for integration tests: real `/bin/sh -c`
  subprocesses per exec channel (genuine exit codes/timeouts/truncation, not simulated) and a real
  `pkg/sftp.Server` rooted at a temp dir. `runExec` runs each child in its own process group and kills the
  whole group after a short idle watchdog — needed because a client that stops draining mid-stream can
  leave a pipeline's grandchildren (e.g. `yes | head`) blocked on a full OS pipe indefinitely; killing just
  the direct shell child doesn't reach them.

## Testing conventions

- Table-driven tests for pure logic (`internal/config`, `internal/pathsafe`, `internal/ignore`,
  `internal/syncengine/plan_test.go`).
- Fakes (not mocks) for failure-injection that's hard to trigger against a real SSH session — see
  `internal/syncengine/apply_test.go`'s `fakeTransfer` for partial-failure / connection-lost scenarios.
- Full end-to-end tests in `internal/tools` spin up a real `sshtest` fixture *and* a real MCP session over
  `mcp.NewInMemoryTransports()`, calling tools through `session.CallTool(...)` exactly as a real client
  would — see `newEnv`/`(*testEnv).call` in `internal/tools/integration_test.go`.
- Many tests carry a short `T-SOMETHING-LIKE-THIS` tag in a leading comment even when the Go function name
  differs — a stable, greppable label for "the test covering this specific scenario." Keep that convention
  when adding a test for a specific named failure mode or edge case.
