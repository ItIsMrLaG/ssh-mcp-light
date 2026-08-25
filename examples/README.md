# Example: one local VM

This directory has the minimum needed to try `ssh-mcp-light` against a
single host reachable over SSH on the default port — by default, the same
machine the server runs on, treated as any other SSH target.

- [`vm.toml`](vm.toml) — declares one VM, named `local`.
- [`myproject.toml`](myproject.toml) — a project config using this
  `examples/` directory itself as the project root, so there's nothing
  else to set up.

## 1. Adjust the config

Edit `vm.toml`:

- `user` — your SSH username on the target host.
- `identity_file` — path to your **unencrypted** private key
  (`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, etc. — write the full path, TOML
  does not expand `~`). A passphrase-protected key is rejected.
- `remote_root` — a directory that user can write to on the target host.
  It will be created if it doesn't exist.

Make sure that key is authorized to log in as that user on that host
(`ssh -i /path/to/key user@127.0.0.1` should work without a password
prompt before you try this).

## 2. Build the server

From the repository root:

```sh
go build -o ssh-mcp-light ./cmd/ssh-mcp-light
```

## 3. Try it by hand (optional)

The server speaks MCP over stdio, so it's not meant to be run
interactively — but you can confirm it starts and validates its config:

```sh
./ssh-mcp-light --project examples/myproject.toml --vm-config examples/vm.toml
```

It should sit waiting for MCP requests on stdin with no output (a fatal
config problem prints a `fatal: ...` line to stderr and exits instead).
Press Ctrl-C to stop it.

## 4. Register it with an MCP client

Both examples below use absolute paths — MCP clients generally launch the
server from an arbitrary working directory, so relative paths to
`--project`/`--vm-config` aren't reliable. Replace `/path/to/ssh-mcp-light`
below with wherever you keep the checkout.

### Claude Code

Add an entry to your project's `.mcp.json` (create one at the repo root if
it doesn't exist), or globally:

```json
{
  "mcpServers": {
    "ssh-mcp-light-example": {
      "command": "/path/to/ssh-mcp-light/ssh-mcp-light",
      "args": [
        "--project", "/path/to/ssh-mcp-light/examples/myproject.toml",
        "--vm-config", "/path/to/ssh-mcp-light/examples/vm.toml"
      ]
    }
  }
}
```

Or via the CLI, which writes the same thing for you:

```sh
claude mcp add ssh-mcp-light-example -- \
  /path/to/ssh-mcp-light/ssh-mcp-light \
  --project /path/to/ssh-mcp-light/examples/myproject.toml \
  --vm-config /path/to/ssh-mcp-light/examples/vm.toml
```

Run `claude mcp list` afterward to confirm it's registered, then ask
Claude something like "list the VMs available to you" in a session — it
should call `host_list` and see `local`.

### Codex CLI

Codex CLI reads MCP server definitions from `~/.codex/config.toml` under an
`mcp_servers` table:

```toml
[mcp_servers.ssh-mcp-light-example]
command = "/path/to/ssh-mcp-light/ssh-mcp-light"
args = [
  "--project", "/path/to/ssh-mcp-light/examples/myproject.toml",
  "--vm-config", "/path/to/ssh-mcp-light/examples/vm.toml",
]
```

(Check `codex --help` / the Codex CLI docs for the current config format
and location if this has changed since — MCP support is still evolving
there.)

## 5. What to expect

Once registered, an agent has five tools scoped to this one VM and this
one project directory:

- `host_list` → `[{"name": "local", "description": "..."}]`
- `path` → this `examples/` directory locally, or
  `/home/user/agents/ssh-mcp-light-example` on the VM
- `exec` → runs any command on `local`, unrestricted
- `push` / `sync` → copies files from `examples/` to
  `/home/user/agents/ssh-mcp-light-example` on `local`, never outside it

To point this at a real project instead of the `examples/` directory
itself, copy `myproject.toml` next to (or into) that project and change
`project_root` to point at it — see the root [`README.md`](../README.md)
for the full config reference.
