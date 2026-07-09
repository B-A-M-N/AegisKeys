# Security Model

AegisKeys is built around one rule: raw secrets should only be visible through
explicit reveal, copy, or child-process injection paths.

## Local-First Boundary

AegisKeys has no hosted sync service. Your vault never leaves the machine.
Network access is explicit and narrow:

- `provider refresh-models` calls a provider's model API when you request it.
- `run` launches the target tool you selected; that child process may make its
  own network calls.
- Normal vault, profile, config, TUI, audit, and adapter-preview operations are
  local.

## Storage

- Secrets are encrypted at rest in `vault.enc` with Argon2id-derived
  AES-256-GCM.
- Provider metadata lives in `providers.json` and is not secret.
- Profile bindings live in `profiles.json` and reference key IDs, not key
  values.
- Config directory permissions are `0700`; config, vault, profile, provider,
  audit, and temp env files are written `0600`.
- Temp env files are written under the AegisKeys temp directory and should be
  removed with `aegiskeys shred-envfile`.

## Display And Output

- Normal display uses masked secrets, for example `sk-or-v1-...91ef`.
- Secrets of eight characters or fewer display as `<hidden>`.
- Full reveal requires an explicit confirmed `key reveal` or `vault reveal`.
- Output that may contain secrets is redacted before display.
- Audit events are metadata-only and do not have a secret field.

## Secret Input

- `key add`, `vault add`, `key rotate`, and `vault rotate` read raw secret
  values through no-echo prompts.
- Raw secret values are not accepted through secret flags, avoiding shell
  history and process-list exposure.
- `init --password` exists for automation but is less secure because the master
  password can appear in shell history or process tables.

## Launch Boundary

AegisKeys injects credentials only into the child process it launches. It does
not export secrets into the parent shell.

Every launch or preview path resolves through the adapter contract gate:

```text
Profile + provider + key
  -> adapter.Render()
  -> ValidateLaunchStrategy
  -> runner.Run / TUI ExecProcess
```

The validator checks:

- adapter contract completeness and truthfulness
- blocked strategy refusal
- manual/guided apps do not receive raw secrets
- profile env cannot override provider credential vars
- raw secret substrings do not appear in argv, preview text, config file
  content, or non-injecting env plans

## Adapter Confidence

- `verified` means render golden, no-secret-leak, config merge/write, and
  launch-smoke gates pass.
- `experimental` means the adapter renders a useful plan and passes no-leak
  checks but lacks full launch proof.
- `guided` means AegisKeys can guide setup but does not inject raw secrets.
- blocked/manual apps are shown honestly and refused where AegisKeys cannot
  safely control credentials.

## File Writes

Config writes use atomic writes, backup-before-overwrite, raw-secret redaction
checks, and symlink/scope protection. Path expansion is restricted to
`HOME`, `XDG_CONFIG_HOME`, and `TMPDIR`; arbitrary ambient environment
expansion is not allowed.

TOML/XML writes refuse to overwrite existing user/project config until
parser-backed merge support exists.

## TUI Memory Boundary

Rendered views and child screen state hold masked data. While unlocked, the
decrypted vault session and derived key are resident in process memory so the
TUI can mutate and launch. `q`, `Ctrl+L`, and `Ctrl+C` route through vault lock
cleanup, which zeroes the derived key and clears decrypted secret strings.

Memory zeroization is best-effort in Go and does not defend against a fully
compromised local machine.

## Threat Model

AegisKeys protects against accidental secret leakage into:

- terminal output
- logs and audit events
- commits and plaintext config files
- broad shell environment exposure
- unsafe file permissions
- unsupported apps receiving secrets

AegisKeys does not fully protect against:

- a compromised OS, kernel, or root account
- malware running as the same user
- malicious target tools that exfiltrate injected env vars
- hardware keyloggers
- shoulder surfing
- terminal scrollback from manually pasted secrets
- OS clipboard managers that capture copied values

## Evidence

Executable evidence is tracked in `SECURITY_EVIDENCE.md`. The release gate also
runs:

```bash
go test ./...
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run . adapter verify
```
