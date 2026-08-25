# Troubleshooting

| Code | Likely cause | Fix |
|---|---|---|
| `E_UNKNOWN_VM` | `vm` isn't a key in `vm.toml` | check `host_list`, fix the name |
| `E_INVALID_ARGUMENT` | a required field is missing, or a value is out of range | check the tool's input schema |
| `E_PATH_TRAVERSAL` | a `push`/`sync` path resolves outside its root | use a path inside the project root / remote base |
| `E_FILE_NOT_FOUND` | a `push` `files` entry doesn't exist under `<project-root>` | check the path is correct and committed locally |
| `E_KEY_MISSING` | `identity_file` doesn't exist on disk | fix the path in `vm.toml` |
| `E_KEY_UNREADABLE` | permission denied, or not a supported key format | fix file permissions / regenerate the key |
| `E_KEY_ENCRYPTED` | the private key has a passphrase | supply an unencrypted key file |
| `E_SSH_CONNECT_FAILED` | TCP dial or handshake failed or timed out (10s budget) | check address/port, firewall, VM is up |
| `E_SSH_AUTH_FAILED` | the VM rejected the key | check the key is authorized for `user` on that VM |
| `E_SFTP_FAILED` | an SFTP operation failed for another reason | check remote disk space / permissions |
| `E_SSH_CONNECTION_LOST` | the connection dropped mid-`push`/`sync` | retry; check network stability |
| `E_TRANSFER_TIMEOUT` | a `push`/`sync` call exceeded its 30-minute budget | split into smaller syncs, check network speed |
| `E_CONCURRENCY_TIMEOUT` | too many concurrent calls to one VM | retry; at most 4 concurrent connections per VM |
