# ssh-mcp-light

A stateless MCP (Model Context Protocol) server that gives an LLM agent
controlled access to a small set of pre-declared, trusted VMs over SSH.
Every tool call resolves its target host and paths from configuration on
disk — there is no free-form host, port, or user parameter anywhere in the
tool surface.

## Install

Requires Go 1.23 or later.

```sh
go build -o ssh-mcp-light ./cmd/ssh-mcp-light
```

## Configuration

Two TOML files are required: a global `vm.toml` declaring every VM this
server installation may reach, and a per-project config file declaring
where that project lives and where it lands on each VM. The project
config's filename is arbitrary — it is identified only by the path you
pass to `--project`.

### `vm.toml`

```toml
# vm.toml
[vms.staging]
address = "10.0.4.12"
port = 22
user = "deploy"
identity_file = "keys/staging_ed25519"   # resolved next to this file if relative
remote_root = "/srv/agents"
description = "Staging box, shared by several projects"

[vms.build-1]
address = "build-1.internal.example.com"
user = "ci"
identity_file = "/home/me/.ssh/build_ed25519"
remote_root = "/home/ci/work"
description = "Dedicated build VM"
```

| Key | Required | Notes |
|---|---|---|
| `address` | yes | hostname or IP, no scheme |
| `port` | no (default `22`) | `1`–`65535` |
| `user` | yes | SSH username |
| `identity_file` | yes | private key path; absolute or relative to `vm.toml`'s directory |
| `remote_root` | yes | absolute remote base directory |
| `description` | no | free text shown by `host_list` |

### Project config file

```toml
# deploy/staging.toml  (an arbitrary filename — only its --project path matters)
project_root = "../.."               # resolved against this file's own directory
remote_local_root = "projects/api"   # interpreted inside <remote-root>

include = ["cmd", "internal", "go.mod", "go.sum"]
ignore = ["*.log", "/tmp/", "internal/testdata/"]
use_gitignore = true
```

| Key | Required | Notes |
|---|---|---|
| `project_root` | yes | absolute, or relative to this file's own directory |
| `remote_local_root` | yes | relative; must not be absolute or contain `..` |
| `include` | no | restricts `sync`'s local enumeration; `push`'s explicit `files` list is exempt |
| `ignore` | no | gitignore-style patterns |
| `use_gitignore` | no (default `false`) | also load every `.gitignore` under `project_root` |

## Running the server

```sh
ssh-mcp-light --project <path> [--vm-config <path>]
```

- `--project <path>` is required. No environment variable, directory walk,
  default filename, or search path is ever consulted for it.
- `vm.toml` is resolved from `--vm-config` if given, else from
  `$VMMCP_CONFIG`. If neither is set, the server refuses to start.
- One process serves exactly one project; run a second instance (with its
  own `--project`) for a second project.

### MCP client registration

```json
{
  "mcpServers": {
    "ssh-mcp-light-api": {
      "command": "ssh-mcp-light",
      "args": [
        "--project", "/home/me/work/api/deploy/staging.toml",
        "--vm-config", "/home/me/.config/ssh-mcp-light/vm.toml"
      ]
    }
  }
}
```

See [`examples/`](examples/) for a runnable one-VM setup, including
registration steps for Claude Code and Codex CLI specifically.

## Tools

Five tools (`host_list`, `path`, `exec`, `push`, `sync`), one worked
example per tool, and an ASCII diagram of how `project_root`,
`remote_root`, and `remote_local_root` combine into the paths each tool
uses: see [`docs/tools.md`](docs/tools.md).

## Path confinement

`push` and `sync` confine every write and every delete to
`<remote-root>/<remote-local-root>`, including through symlinked
ancestors and traversal attempts (`..`, absolute paths, remote symlinks).
`exec` is the opposite by design: it runs any command with any arguments,
anywhere on the VM, with no path resolution or filtering applied to it at
all. Don't add confinement to `exec` — that is intentional, not a gap.

## Limitations

- **No host key verification.** The client accepts whatever key the SSH
  server presents; there is no known-hosts store or pinning in this
  version. `internal/sshlayer.HostKeyPolicy` is the one seam a
  trust-on-first-use or pinning implementation would plug into later,
  without touching the tool surface.
- **No authentication layer.** Over stdio, the server is spawned by the MCP
  client under the invoking user's own OS account — scope (which VMs, which
  remote subtree), not identity, is the control that matters here.
- **No SSH agent, no passwords, no keyboard-interactive.** Private key file
  only; passphrase-protected keys are rejected (`E_KEY_ENCRYPTED`).

## Troubleshooting

Every `E_*` error code, its likely cause, and the fix: see
[`docs/troubleshooting.md`](docs/troubleshooting.md).

## Testing

```sh
go test ./...
```

No network access beyond `localhost`, no real VM, and no secrets: the
integration tests spin up an in-process SSH+SFTP server
(`internal/sshtest`) backed by a temp directory and an ephemeral key
generated at test time.
