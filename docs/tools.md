# Tools

Five MCP tools: `host_list`, `path`, `exec`, `push`, `sync`. Every VM is
selected by the logical name declared in `vm.toml` — no tool takes a
host, port, or user parameter.

## How the config keys become paths

Two roots come from config, and everything else is built from them.

```
project config file (any filename)          vm.toml
---------------------------------           --------------------------------
project_root        = "../.."               [vms.staging]
remote_local_root   = "projects/api"           remote_root = "/srv/agents"


1) <project-root>   (LOCAL)
   = project_root, resolved against *the config file's own directory*
     (not the process's cwd, not the config file's name)
   = /home/me/work/api

2) <remote-root>    (per VM, from vm.toml -- shared by every project on it)
   = /srv/agents

3) <remote-base>    (THIS project's own corner of that VM)
   = <remote-root> / <remote-local-root>
   = /srv/agents/projects/api
```

`remote_local_root` is owned by the *project*, not the VM, precisely so
several projects can share one VM — each under its own subdirectory of
`<remote-root>` — without their files colliding. `exec` is the one tool
that ignores all of this: it runs anywhere on the VM by design — see
[Path confinement](../README.md#path-confinement).

## `host_list`

```json
{}
```
```json
{ "vms": [ { "name": "staging", "description": "Staging box" } ] }
```

## `path`

```json
{}
```
```json
{ "path": "/home/me/work/api", "resolved_target": { "vm": null } }
```

```json
{ "vm": "staging" }
```
```json
{ "path": "/srv/agents/projects/api", "resolved_target": { "vm": "staging", "remote_base": "/srv/agents/projects/api" } }
```

## `exec`

Unrestricted: no filtering, no path confinement. Any command, anywhere on
the VM.

```json
{ "vm": "staging", "cmd": "systemctl", "args": ["status", "api"] }
```
```json
{ "stdout": "...", "stderr": "", "exit_code": 3, "timed_out": false, "truncated_stdout": false, "truncated_stderr": false, "resolved_target": { "vm": "staging", "cwd": "/srv/agents" } }
```

## `push`

```json
{ "vm": "staging", "files": ["cmd/api/main.go", "go.mod"], "dest": "./" }
```
```json
{ "uploaded": ["cmd/api/main.go", "go.mod"], "skipped_by_ignore": [], "failed": [], "resolved_target": { "vm": "staging", "remote_base": "/srv/agents/projects/api" } }
```

## `sync`

```json
{ "vm": "staging", "dry_run": true }
```
```json
{ "dry_run": true, "to_upload": ["cmd/api/main.go"], "to_delete": ["old_binary"], "uploaded": [], "deleted": [], "skipped_symlinks": [], "protected_by_ignore": [], "failed": [], "resolved_target": { "vm": "staging", "remote_base": "/srv/agents/projects/api" } }
```

Drop `dry_run` (or set it `false`) to actually upload and delete.
