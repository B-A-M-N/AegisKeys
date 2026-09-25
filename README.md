# AegisKeys

A secure local vault for API provider metadata and secrets. Stores providers and
keys **separately**, then injects the correct credentials into coding agents and
CLIs as **child-process-scoped** configuration on demand — env vars, CLI args,
and config files, rendered per-app through an **adapter contract layer**.

![AegisKeys TUI demo](docs/demo/tui-matrix-logo.gif)

Watch the full end-to-end demo: [`docs/demo/full-flow-launch.mp4`](docs/demo/full-flow-launch.mp4)

## What it does

- **Providers** — non-secret metadata (base URL, env var name, auth header
  template, model catalog, capabilities). Multiple providers seeded by default; add your own.
- **Secrets** — API keys stored **encrypted at rest** (Argon2id + AES-256-GCM).
- **Profiles** — bind a provider to a key, target app, plus per-app model slots and runtime env.
- **Adapter contracts** — each supported app declares its support level,
  credential control model, model slots, and hazards; adapters render profiles
  into launch strategies with env, args, and config files.
- **Run** — launch any command with the profile's secrets injected into the
  child process only. Global shell is never touched; config files are written
  with atomic 0600 writes, backup, and redaction checks.
- **TUI** — interactive terminal UI with dashboard, providers, vault, profiles,
  contract-aware launch screen, doctor, audit, settings, Access, scratch, and help.
- **Credential broker** — optional local Unix-socket service lets authorized
  apps resolve stable binding names, so one vault rotation reaches the next
  provider-client reconnect without rewriting `.env` files.

## Feature tour

### Vault and secret lifecycle

- Encrypts vault contents with Argon2id-derived keys and AES-256-GCM.
- Supports password unlock, optional OS-keyring convenience unlock, and
  explicit keyring-required vaults with protected recovery files.
- Keeps provider metadata and secret records in separate stores so rotating a
  credential does not rewrite application profiles.
- Supports primary secrets, named secondary components, private notes,
  labels, tags, expiration metadata, rotation reminders, and per-record
  reveal/copy/export/launch policy.
- Provides transactional mutations, backup/rekey/repair flows, and fail-closed
  handling for malformed or resource-exhausting vault envelopes.

### Provider and model management

- Ships with a broad catalog of hosted, OpenAI-compatible, Anthropic-compatible,
  Google-compatible, and local providers.
- Stores base URLs, auth specifications, endpoint templates, capabilities, and
  model catalogs as non-secret metadata.
- Validates remote destinations, endpoint substitutions, redirect policy, and
  credential-origin decisions before a provider request can carry a secret.
- Refreshes supported model catalogs with bounded response parsing and writes
  the refreshed catalog atomically.
- Supports local providers such as Ollama and LM Studio without requiring a
  hosted credential.

### Profiles and launch configuration

- Binds a provider, vault key, target app, model roles, and optional runtime
  environment into a reusable profile.
- Provides app-specific model slots such as main/fast/weak/editor/planner/actor
  and adapter-specific catalog, fallback, summary, and subagent roles.
- Resolves a profile through the same mandatory contract gate for CLI previews,
  CLI launches, TUI launches, saves, and tests.
- Supports command overrides, working directories, environment variables, and
  explicit arbitrary-command launches where the adapter permits them.
- Renders parser-backed JSON, JSONC, YAML, TOML, and XML configuration overlays
  while preserving unrelated user settings.
- Provides conflict-aware cleanup: AegisKeys restores its own unchanged overlay
  after launch, while preserving child/user modifications and reporting
  conflicts.

### Adapter catalog and honest support

- Includes adapters for generic launches, Aider, Crush, Qwen Code, Goose,
  Claude Code, Hermes, Cline, Mistral Vibe, Codex, MiMo, OpenCode, OpenHands,
  Gemini CLI, Copilot CLI, Continue, Zed, IntelliJ IDEA, Roo, Kilo, and Cursor.
- Declares credential-control mode, launch surface, model slots, hazards,
  validation checks, and manual steps in an explicit `AppSupportContract`.
- Runs render, no-secret-leak, config-merge, and fake-executable launch gates.
- Keeps synthetic verification separate from version-pinned real-app
  qualification; `verified` never claims universal third-app compatibility.
- Falls back to generic visual identity artwork for unreviewed third-party logo
  assets without changing the app’s integration or model behavior.

### Interactive TUI

- Provides dashboard, provider, key, profile, launch, doctor, audit, settings,
  Access, scratchpad, and help screens.
- Uses asynchronous commands for slow vault, doctor, model, and access
  operations rather than blocking the terminal event loop.
- Fences stale unlock, doctor, editor, approval, and launch results against the
  active vault session generation.
- Supports idle auto-lock, explicit lock, derived-key zeroization, model
  selection, scratchpad notes, and optional external-editor handoff.
- Presents a Matrix-rain logo reveal using a generic identity fallback when a
  dedicated third-party illustration is not publication-approved.

### Credential broker and access control

- Exposes an optional Linux-only Unix-domain broker; it does not listen on TCP.
- Authenticates peers with Linux UID plus executable path/hash constraints and
  requires an independent secret policy for every resolve/rotate operation.
- Supports named bindings, expiring grants, per-grant capability and
  credential-component allowlists, interpreter-wide acknowledgement, and
  rotation through a dedicated broker operation.
- Keeps broker metadata separate from raw vault values and fails closed on
  unexpected pre-existing listeners or unsafe runtime paths.
- Reconciles grant-derived policy when grants expire, are revoked, rebind, or
  are deleted, while preserving administrative policy.

### Diagnostics, audit, and recovery

- Records metadata-only audit events for vault, provider, profile, launch,
  broker, doctor, and access operations.
- Validates audit event schemas, redacts secret-shaped output, bounds reads,
  rotates oversized logs, and reports write failures.
- Provides `doctor`, adapter verification, encrypted-vault backup, repair-unlock,
  rekey, and recovery-oriented diagnostics.
- Uses secure atomic storage for sensitive files and no-follow filesystem
  primitives on supported Unix and Windows targets.

### Portability and release tooling

- Builds on Linux, macOS, and Windows; the credential broker is intentionally
  Linux-only because peer identity uses Linux `SO_PEERCRED`.
- Uses portable `charm.land/.../v2` dependencies without machine-local module
  replacements.
- Pins release actions to reviewed commit SHAs, validates release inputs and
  signed tags, verifies artifact checksums, and keeps per-asset logo review
  separate from application compatibility.

## Why this exists

Personally, I kind of suck at keeping track of this kind of stuff. For the
longest time my API keys and model configs lived in a graveyard of
half-forgotten `.env` files, shell snippets, and notes scattered across
machines.

Rather than keep storing things badly, I built one local vault where I can drop
keys from the providers I use — OpenRouter, OpenAI, Anthropic, Google, plus
local Ollama and LM Studio setups — and then point a coding agent at a profile
that gives it exactly the credentials it needs. No more re-pasting keys, no
more hunting through shell history for which base URL goes with which model,
and no more wondering whether that `.env` I just committed had a secret in it.

It's the tool I wished I had every time I spun up a new agent and groaned at
the thought of wiring credentials in by hand again.

## Third-party identity notice

Third-party application names and any dedicated marks used in compatibility
screens identify supported integrations only. AegisKeys is independent and does
not imply sponsorship or endorsement. Dedicated artwork is subject to the
per-asset review manifest; unresolved assets use the generic identity fallback.
In the current release build, only the generic identity artwork is embedded;
dedicated third-party illustrations remain source-tree review artifacts until
individually approved. The fallback affects presentation only, not the app's
name, adapter, integration, or model configuration.

## Supported apps

Verified support means the adapter has render goldens, no-secret-leak checks,
config merge/write checks where applicable, and fake-executable launch smoke.
Experimental adapters are useful, but they should not be treated as equivalent
to the verified surface until their contracts have the same proof.

### Verified

| App | Support mode | Credential control | Model slots |
|-----|-------------|--------------------|-------------|
| Aider | full env | env injection | main, weak, editor |
| Crush | env + config | env + `crush.json` merge | main, catalog |
| Qwen Code | env + config | env + `settings.json` merge | main, catalog, fallbacks |
| Goose | env + config | env + `config.yaml` merge | main, fast |
| Claude Code | full env (OAuth warning) | env injection | main |
| Generic | env + args | env injection | main |

### Experimental

These adapters render launch/config plans and pass no-secret-leak checks, but
do not yet have full verified launch proof.

| App | Support mode | Credential control | Model slots |
|-----|-------------|--------------------|-------------|
| Hermes | env + config | env injection + isolated `HERMES_HOME` | main, compression, vision, web_extract |
| Cline CLI | env + config | env + `providers.json` merge | planner, actor |
| Free Claude (`free-code`) | env | Anthropic Messages gateway | main, fast, planner, subagent |
| Mistral Vibe | env + config | env + `config.toml` merge | main |
| Codex CLI | full env | env injection | main plus configurable Codex model aliases |
| MiMo | env + config | env injection + config patch | main |
| OpenCode | env + config | env injection + config patch | main |
| OpenHands | env + config | env injection + config patch | main |
| Gemini CLI | env + config | env injection + config patch | main |
| Copilot CLI | full env | env injection | main |
| Continue | env + config | env injection + config patch | main |

### Guided / Manual

These apps cannot safely receive raw secrets through AegisKeys alone. AegisKeys
can guide config/model setup, but credential entry remains manual or keychain
based.

| App | Support mode | Credential control | Model slots |
|-----|-------------|--------------------|-------------|
| Roo Code | manual extension setup | guided manual handoff; no raw secrets written | main |
| Kilo Code | manual extension setup | guided manual handoff; no raw secrets written | main |
| Zed | config + keychain (partial) | keychain handoff; no raw secrets written | main, inline_assistant, subagent, commit_message, thread_summary, alternatives |
| IntelliJ IDEA | launcher/config isolation | manual PasswordSafe handoff | main |

### Blocked / Manual

| App | Reason | Path |
|-----|--------|------|
| Cursor | account-based auth; no safe secret injection path | configure credentials in Cursor settings |

**Confidence levels:** `experimental` = adapter renders but has no real launch proof; `manual_proof` = a maintainer/user has recorded a successful real launch with a pinned provider/model, but automated gates have not all passed; `verified` = automated contract gates passed (render golden, no-secret-leak, config merge, fake-executable launch smoke). `verified` is not proof that every current third-party app accepts the generated configuration; real-app compatibility requires a version-pinned qualification record. `guided` = config/model setup only, credential handoff is manual/keychain.

Each app's contract declares its hazards (e.g. "Aider loads `.env` files that
can shadow injected secrets"; "Zed macOS app bundle may not inherit env vars").

`free-code` supports its own OpenAI Codex mode, but that is not a generic
OpenAI-compatible gateway setting. For OpenCode Zen/Go profiles, AegisKeys
uses the provider's Anthropic Messages endpoint when available and starts an
ephemeral loopback Anthropic-to-OpenAI bridge for chat-completions-only models.
The provider API key remains in the launcher process; Free Code receives only
a local bridge credential.

## Design philosophy

Developers juggle many coding agents, model providers, routers, runners, and
IDE-based AI tools. Secrets scatter across `.env` files, shell history, IDE
settings, and random scripts. AegisKeys is the secure local bridge: one
encrypted vault, precise per-command injection, per-app config file rendering,
full audit trail — without global secret exposure. It knows when env injection
is enough, when config writing is required, when keychain/OAuth break the model,
and when it should refuse to pretend an app is safely supported.

## Security model

AegisKeys is built around one rule: **raw secrets should only be visible through
explicit reveal, copy, or child-process injection paths.**

- Local-first: no hosted sync service. The vault never leaves the machine.
- Network is explicit: AegisKeys only makes provider API calls when you ask it
  to refresh provider metadata; launched target tools may make their own calls.
- Secrets are encrypted at rest with Argon2id + AES-256-GCM.
- Providers and profiles are non-secret metadata; raw key material stays in
  `vault.enc`.
- Injection is child-process-scoped. The parent shell is not exported into.
- Normal output is masked and redacted; full reveal requires confirmation.
- Adapter contracts are enforced before any secret is trusted or handed off.
- Config writes use locked-down permissions, backups, redaction checks, and
  fail-closed merge behavior for unsupported formats.

See `docs/security.md` for the detailed security model, threat boundaries, and
evidence map.

### Threat model

AegisKeys protects against accidental secret leakage into terminal output, logs,
commits, plaintext storage, broad shell env exposure, unsafe file permissions,
and local snooping by non-privileged users.

It does **not** fully protect against a fully compromised machine, kernel-level
malware, malicious coding tools that exfiltrate injected env vars, shoulder
surfing, terminal scrollback of manually-pasted secrets, hardware keyloggers, or
an OS/clipboard manager that captures a copied value.

## Install / build

```bash
go build -buildvcs=false -o aegiskeys .
make install PREFIX="$HOME/.local"
make release VERSION=0.1.0
```

Requires Go 1.25.13 or newer. Dependencies resolve from `go.mod`/`go.sum`; no
machine-local module replacement paths are required.

Maintainer release steps are documented in `docs/release.md`.

Shell completions are generated with:

```bash
aegiskeys completion bash
aegiskeys completion zsh
aegiskeys completion fish
aegiskeys completion powershell
```

## Quickstart

```bash
./aegiskeys init                              # create vault + seed providers (interactive password)
# Automation: pipe the password through a protected input descriptor instead
printf %s "$PW" | ./aegiskeys init            # avoids putting the password in argv/history
./aegiskeys key add --provider openrouter     # add an API key (encrypted)
./aegiskeys profile create --name or-main \
    --provider openrouter --key <key-id> \
    --app aider \
    --model-main claude-sonnet-4-5             # bind provider + key + app + model
./aegiskeys run --profile or-main -- aider   # launch aider with secrets injected
```

Running `aegiskeys` with no subcommand opens the TUI.

Only launch commands you trust. AegisKeys scopes credentials to the child
process and validates its own render surfaces, but the child can still read and
misuse its injected environment.

## TUI usage

```
aegiskeys              # open the interactive UI
aegiskeys tui          # same
```

Keys: `1`–`8` jump to a screen, `Tab` cycles, `j`/`k` scroll, `r` runs the
doctor, `Ctrl+L` locks the vault (clears derived key + decrypted secrets from
memory), `q`/`Ctrl+C` quits.

On the Keys screen, press `e` to **edit/rename** the selected key (label,
provider, tags) and `r` to rotate its secret. Stale keys (older than the
**rotation reminder** interval set in Settings) show a `⚠ rotate` badge. Use
`←`/`→` or `Enter` on the **Settings** screen to adjust theme, auto-lock,
clipboard TTL, animations, risky export, rotation reminders, and runtime policy.

The Launch screen shows contract info for the selected profile and the resolved
launch plan (command, args, files, hazards). Press `Enter` to launch with the
adapter default command, or `Right`/`d` to type a command override. The TUI
releases the terminal while the child runs and resumes afterward.

## Demos

Reproducible terminal demos are recorded with VHS tapes in `demos/vhs/`.

```bash
make demo      # build and render all demo media
make demo-tui  # render only the TUI/matrix-logo reveal demo
make demo-cli  # render only the CLI overview demo
make demo-full # render the slower full-flow launch + scratchpad demo as MP4
```

Generated media is written to `docs/demo/`. The tapes use throwaway config
directories under `tmp/` and fake demo passwords/API keys only.

## CLI examples

```bash
# Providers
aegiskeys provider list
aegiskeys provider inspect openrouter
aegiskeys provider models openrouter
aegiskeys provider refresh-models openrouter --key <key-id>
aegiskeys provider add --slug myllm --name "My LLM" --base-url https://my.api/v1 \
    --env-var MY_API_KEY

# FreeCodex without an AegisKeys profile
# The vault label/ID is selected at launch; no FreeCodex profile is created and
# the secret is injected only into the child process environment.
aegiskeys with --key <vault-key> --env OPENROUTER_API_KEY -- freecodex models
aegiskeys with --key <vault-key> --env OPENROUTER_API_KEY -- freecodex
#

# Keys (always masked unless explicitly revealed)
aegiskeys key list
aegiskeys key show --id <id>
aegiskeys key reveal --id <id>       # confirmation required
aegiskeys key rotate --id <id>
aegiskeys key rename --id <id> --label new-name

# Profiles
aegiskeys profile create --name or-main \
    --provider openrouter --key <key-id> \
    --app aider --model-main claude-sonnet-4-5 --model-weak gpt-4o-mini
aegiskeys profile list
aegiskeys profile inspect or-main     # shows adapter contract, slots, hazards
aegiskeys profile inspect zed-prof --app zed

# Injection (uses adapter contracts under the hood)
aegiskeys run --profile or-main -- aider
aegiskeys env --profile or-main               # masked preview
aegiskeys env --profile or-main --export      # full exports (confirmation)
aegiskeys envfile --profile or-main           # write a 0600 temp env file
aegiskeys shred-envfile <path>                # overwrite + delete

# Vault items and manual handoff
aegiskeys vault list
aegiskeys vault copy --id <id>                # confirmation + clipboard policy
aegiskeys vault backup                        # encrypted backup, no decryption
aegiskeys vault rekey                         # reseal with current KDF params
aegiskeys handoff --profile zed-prof          # guided manual/keychain flow

# Settings (also editable in the TUI Settings screen)
aegiskeys settings show
aegiskeys settings set auto_lock_minutes 30
aegiskeys settings set rotation_reminder_days 90   # flag stale keys in the TUI
aegiskeys settings set runtime_policy standard     # allow risky export (envfile)
aegiskeys settings reset

# Diagnostics
aegiskeys doctor
aegiskeys audit -n 20
aegiskeys adapter verify                      # render/files/no-leak, no target app install required
aegiskeys adapter verify --installed          # optional local installed-CLI smoke
```

> **Runtime policy & risky export.** The `runtime_policy` setting governs
> dangerous operations. It defaults to `strict`, which **refuses** to write
> secrets to a plaintext env file (`envfile`). Set it to `standard` or
> `permissive` to allow `envfile` (still subject to per-key policy and the
> confirmation prompt). `theme`, `auto_lock_minutes`, `clipboard_ttl_seconds`,
> `adapter_verify_timeout_seconds`, and `enable_risky_export` are also
> adjustable.

## Architecture

**Three-domain model + adapter layer:**

| Domain | Package | On Disk | Secret? |
|--------|---------|---------|---------|
| **Providers** (metadata) | `internal/provider` | `providers.json` | No |
| **Secrets** (API keys) | `internal/secret` | `vault.enc` (encrypted) | Yes |
| **Profiles** (binding) | `internal/profile` | `profiles.json` | No (refs key id) |
| **Adapter** (rendering) | `internal/adapter` | — | No |
| **Runner** (execution) | `internal/runner` | — | No |

**Profile → Launch Strategy flow:**

```
Profile (provider + key + models + target app)
  → ResolveLaunchStrategy(profile, provider, key, adapterRegistry)
    → adapter.Render() → LaunchStrategy{Plan, Support, ManualSteps, Hazards}
      → ValidateLaunchStrategy → ValidateContract   # mandatory gate, every path
        → runner.Run() applies Plan.Command/Args/Env/Files
```

Every resolve — `run`, `env`, `envfile`, the TUI launch screen, and tests —
flows through the same mandatory gate. `ValidateContract` ensures an adapter
honestly declares its support level, credential control, and hazards before any
contract field (or secret) is trusted; a mis-declared or guided adapter is
refused secrets rather than silently trusted.

Each adapter publishes an `AppSupportContract` declaring its support level,
credential control, model slots, and hazards. The CLI and TUI both consume
these contracts for validation, preview, and safe execution.

File writes use atomic writes with backup-before-overwrite, parser-backed
JSON/YAML/JSONC/TOML merges, identity-aware XML patching, and raw-secret
redaction checks.

## Configuration

Everything lives under `~/.config/aegiskeys` (override with `--config <dir>` on
any command):

```
~/.config/aegiskeys/
├── config.json     0600   preferences (see below)
├── providers.json  0600   provider metadata (plaintext, non-secret)
├── profiles.json   0600   profile bindings (reference key ids, no secrets)
├── vault.enc       0600   encrypted secrets (Argon2id → AES-256-GCM)
├── audit.log       0600   append-only event log (metadata only)
└── tmp/            0700   temporary env files (written 0600, shred with `shred-envfile`)
```

Settings are edited with `aegiskeys settings set <key> <value>` (or the TUI
Settings screen, `←`/`→` or `Enter` to adjust) and persist to `config.json`:

| Setting | Default | Meaning |
|--------|---------|---------|
| `theme` | `vault` | TUI color theme |
| `auto_lock_minutes` | `15` | idle auto-lock; `0` disables |
| `default_profile` | — | profile used when a command omits `--profile` |
| `clipboard_ttl_seconds` | `45` | ceiling for clipboard copy lifetime (per-key policy may lower it) |
| `adapter_verify_timeout_seconds` | `20` | adapter preflight timeout (clamped 1–300) |
| `enable_risky_export` | `false` | only honored under a non-strict `runtime_policy` |
| `rotation_reminder_days` | `90` | `0` disables the `⚠ rotate` badge on stale keys |
| `runtime_policy` | `strict` | `strict` \| `standard` \| `permissive`; `strict` blocks `envfile` |

> Changing `runtime_policy` to `strict` force-disables `enable_risky_export`.
> Reset everything with `aegiskeys settings reset`.

## Provider / key / profile model

- A **provider** declares *how* to talk to an API (which env var holds the key,
  the base URL, auth spec, model catalog, capabilities).
- A **key** is the secret itself, encrypted, tagged with a provider slug.
- A **profile** says "use *this* key with *this* provider, targeting *this*
  app, with *these* model roles, plus extra env vars" — and is the unit you
  launch with.

## OpenAI-compatible and local providers

Routers (OpenRouter, Together, Fireworks, DeepSeek, Moonshot, Qwen, Groq, Nvidia
NIM, ModelScope) inject both their own key var and `OPENAI_BASE_URL` so
OpenAI-SDK clients work unchanged.

Local providers (Ollama, LM Studio) inject only `OPENAI_BASE_URL` — no key needed.

## TUI status

The TUI covers dashboard, providers, keys, profile wizard, contract-aware
launch, doctor, audit, settings, and help. Launch uses the same runner
preparation path as the CLI and releases the terminal while the child process
runs. See `TUI_GUIDE.md` for the full screen architecture.

## OS keyring unlock

Password-derived vaults can opt into OS-keyring convenience unlock with
`aegiskeys keyring-enable`; the password remains a recovery method. For an
explicit keyring-only vault, use `aegiskeys keyring-required --recovery-file
<new-path>` and safeguard the new 0600 recovery file. `keyring-recover` can
restore a lost OS-keyring entry from that recovery file without printing its
contents. Use `keyring-status` to inspect the current mode.

## Limitations

- No per-profile policy rules yet (global runtime policy and secret-rotation reminders are now available in Settings).
- GUI/IDE adapters (Zed, IntelliJ) are partial: they configure model slots and isolate config but rely on keychain/manual credential handoff.
- `run` executes binaries directly, not via shell. Pipes (`|`) and built-ins (`export`) require explicit wrapping: `run --profile <x> -- sh -c "..."`.
- While unlocked, the derived vault key and decrypted secrets are resident in process memory (lock or quit to clear).

## Future work

See `docs/future-work.md` for deferred stable/post-stable work. Highlights:
hardware-backed unlock, per-profile policy rules, provider
health checks, secure import/export, team mode, shell-plugin integration, full
IDE adapter coverage, automatic temp-env cleanup, richer TUI themes, audit
viewer filters and signed
release provenance.

## Tests

```bash
go test ./...
go test -race ./...
go build -buildvcs=false ./...
go vet ./...
test -z "$(gofmt -l .)"
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
go run . adapter verify
make release VERSION=0.1.0
sha256sum -c dist/SHA256SUMS
```

`adapter verify` is synthetic contract evidence. It is intentionally not a
substitute for the version-pinned real-application records under
`testdata/adapter_qualification/`.

See `docs/testing.md` for the coverage map and `docs/release.md` for the
complete public release checklist, artifact build, tag, and GitHub release
workflow.

## Infrastructure acknowledgment

AegisKeys was independently developed and is not intended to encourage people
to use FreeInference instead of paid or self-hosted inference. While developing
and validating AegisKeys, I used model-inference access provided by
[FreeInference.org](https://freeinference.org/).

For clarity, FreeInference did not commission, direct, review, endorse, or pay
for AegisKeys. This is not a sponsored contribution or an endorsement by
FreeInference or the AegisKeys project.

I am acknowledging FreeInference because access to capable inference
infrastructure can enable meaningful open-source work by developers and
researchers who do not have the hardware or budget to run these models
themselves. If AegisKeys has been useful to you, please consider supporting
FreeInference through the donation options on their website.
