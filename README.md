# AegisKeys

A secure local vault for API provider metadata and secrets. Stores providers and
keys **separately**, then injects the correct credentials into coding agents and
CLIs as **child-process-scoped** configuration on demand — env vars, CLI args,
and config files, rendered per-app through an **adapter contract layer**.

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
- **TUI** — interactive terminal UI with dashboard, providers, keys, profiles,
  contract-aware launch screen, doctor, audit, settings, and help.

## Why this exists

Personally, I kind of suck at keeping track of this kind of stuff. For the
longest time my API keys and model configs lived in a graveyard of
half-forgotten `.env` files, shell snippets, and notes scattered across
machines — the same not-so-great places we all keep pretending are fine.

Lately I've been doing a lot of TUI work, so rather than keep storing things
badly, I built something genuinely useful for myself: one local vault where I
drop keys from the various providers I use — OpenRouter, OpenAI, Anthropic,
Google, plus the local Ollama and LM Studio setups — and then just point a
coding agent at a profile and let it pull exactly the credentials it needs. No
more re-pasting keys, no more hunting through shell history for which base URL
goes with which model, no more wondering whether that `.env` I just committed
had a secret in it.

It's the tool I wished I had every time I spun up a new agent and groaned at
the thought of wiring credentials in by hand again.

## Supported apps

| App | Support mode | Confidence | Credential control | Model slots |
|-----|-------------|------------|--------------------|-------------|
| Aider | full env | verified | env injection | main, weak, editor |
| Hermes | env + config | experimental | env injection + isolated `HERMES_HOME` | main, compression, vision, web_extract |
| Crush | env + config | verified | env + `crush.json` merge | main, catalog |
| Qwen Code | env + config | verified | env + `settings.json` merge | main, catalog, fallbacks |
| Goose | env + config | verified | env + `config.yaml` merge | main, fast |
| Cline CLI | env + config | experimental | env + `providers.json` merge | planner, actor |
| Claude Code | full env (OAuth warning) | verified | env injection | main |
| Mistral Vibe | env + config | experimental | env + `config.toml` merge | main |
| Codex CLI | full env | experimental | env injection | main, gpt54, gpt54mini, gpt53codex, gpt52codex, gpt52, gpt51codexmax, gpt51codexmini |
| MiMo | env + config | experimental | env injection + config patch | main |
| OpenCode | env + config | experimental | env injection + config patch | main |
| OpenHands | env + config | experimental | env injection + config patch | main |
| Gemini CLI | env + config | experimental | env injection + config patch | main |
| Copilot CLI | full env | experimental | env injection | main |
| Continue | env + config | experimental | env injection + config patch | main |
| Roo Code | manual extension setup | experimental | guided manual handoff; no raw secrets written | main |
| Kilo Code | manual extension setup | experimental | guided manual handoff; no raw secrets written | main |
| Cursor | blocked/manual | experimental | account-based auth; no injection | manual only |
| Zed | config + keychain (partial) | guided | keychain handoff; no raw secrets written | main, inline_assistant, subagent, commit_message, thread_summary, alternatives |
| IntelliJ IDEA | launcher/config isolation | guided | manual PasswordSafe handoff | main (guided) |
| Generic | env + args | verified | env injection | main |

**Confidence levels:** `experimental` = adapter renders but no real launch proof; `manual_proof` = user has launched it successfully with a real provider/model and has fake-executable launch smoke, but automated gates have not all passed; `verified` = tested end-to-end with secret-non-leak assertions AND all verification gates passed (render golden, no-secret-leak, config merge, launch smoke); `guided` = config/model setup only, credential handoff is manual/keychain.

Each app's contract declares its hazards (e.g. "Aider loads `.env` files that
can shadow injected secrets"; "Zed macOS app bundle may not inherit env vars").

## Design philosophy

Developers juggle many coding agents, model providers, routers, runners, and
IDE-based AI tools. Secrets scatter across `.env` files, shell history, IDE
settings, and random scripts. AegisKeys is the secure local bridge: one
encrypted vault, precise per-command injection, per-app config file rendering,
full audit trail — without global secret exposure. It knows when env injection
is enough, when config writing is required, when keychain/OAuth break the model,
and when it should refuse to pretend an app is safely supported.

## Security model

AegisKeys is built around one rule: **the raw secret should never be visible,
persisted, or handed to something that didn't earn it.** Everything below is in
service of that.

- **Local-first** — no remote sync, no network calls. Your vault never leaves
  the machine.
- **Secrets encrypted at rest** — a password-derived Argon2id key seals an
  AES-256-GCM envelope (`vault.enc`). There is no OS keyring dependency (by
  design for the MVP); the master password *is* the key.
- **Provider metadata separated from key material** — providers are plaintext
  config; the secret blob never is. A leaked `providers.json` leaks nothing.
- **Child-process-scoped injection** — secrets land only in the spawned child's
  environment. The parent shell and every other process are untouched, and the
  child's exit code is preserved.
- **Masked by default** — display shows `sk-or-v1-...91ef`; secrets ≤8 chars
  show `<hidden>`. Full reveal requires an explicit, confirmed `key reveal` /
  `vault reveal`.
- **Clipboard is policy-gated** — `vault copy` requires the key's policy to
  allow clipboard access, a confirmation, and respects a per-key
  `MaxClipboardTTLSeconds` (the global `clipboard_ttl_seconds` is a ceiling).
  The tool warns that the OS/clipboard manager may log it.
- **No secret argv flags** — `key add` / `vault add` read secrets only through
  no-echo prompts. Secrets are never accepted as command-line flags, so they
  can't leak into shell history or `ps`. The one exception is `init --password`
  (for non-interactive automation), which accepts the master password directly
  and is clearly marked "less secure — visible in shell history."
- **Hard boundary enforcement** — every launch resolve (`run`, `env`,
  `envfile`, and the TUI) flows through `ValidateLaunchStrategy`, which calls
  `ValidateContract`. An adapter must *honestly declare* its support level,
  credential control, and hazards before any secret is trusted or handed off.
  Manual/guided adapters (Zed, IntelliJ) receive no raw secrets — only
  keychain/manual handoff instructions.
- **TUI zero-leak guarantees** — rendered views and modals hold only masked
  data. While unlocked, the decrypted vault session and derived key are
  resident in memory, but `q` / `Ctrl+L` / `Ctrl+C` all route through
  `lockVault`, which zeroes the derived key and clears decrypted secrets.
  Adversarial tests chase the raw secret across TUI views, adapter renders, and
  modals to prove it never appears.
- **Safe file permissions** — config dir `0700`, files `0600`, temp env dir
  `0700`, temp files `0600`.
- **File-write safety** — atomic writes, backup-before-overwrite, raw-secret
  redaction checks, and scope/symlink protection (`rejectSymlinkParents` walks
  every parent; paths only expand `HOME`/`XDG_CONFIG_HOME`/`TMPDIR`, never
  ambient env). TOML/XML config writes **refuse to overwrite** existing
  user/project config until a real parser-backed merge exists (fail closed).
- **Fail closed** — decryption errors are explicit ("wrong password?");
  tampered ciphertext fails to open.
- **Audit metadata only** — the append-only `audit.log` records events
  (add/rotate/launch/lock) but never key values.

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

Requires Go 1.25.12 or newer. Dependencies resolve from `go.mod`/`go.sum`; no
machine-local module replacement paths are required.

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
./aegiskeys init --password "$PW"             # non-interactive; ONLY for automation (visible in shell history/process table)
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

File writes use atomic writes with backup-before-overwrite, JSON/YAML/JSONC
merge support, and raw-secret redaction checks. TOML/XML writes refuse to
overwrite existing user/project config until a real parser-backed merge exists.

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

## TUI feature status

| TUI Feature | Status |
|---|---|
| Provider list/inspect | working |
| Provider add/remove | working |
| Key list/inspect | working |
| Key add (experimental) | working — secret masked, moved to vault on save |
| Key edit/rename | working — TUI and CLI support rename/tags |
| Profile wizard (app-first) | working — app/provider/key/model-slot collection with validation |
| Launch execution | working — TUI uses the same runner preparation path and `tea.ExecProcess` |
| Doctor | working — text and `--json` output |
| Audit viewer | working |
| Settings | working — all settings wired (theme, auto-lock, animations, risky export, rotation reminders, runtime policy) |
| `Ctrl+L` vault lock | working — zeroes derived key + clears decrypted secrets |

## Limitations

- No OS keyring integration (password-derived encryption only).
- No per-profile policy rules yet (global runtime policy and secret-rotation reminders are now available in Settings).
- GUI/IDE adapters (Zed, IntelliJ) are partial: they configure model slots and isolate config but rely on keychain/manual credential handoff.
- `run` executes binaries directly, not via shell. Pipes (`|`) and built-ins (`export`) require explicit wrapping: `run --profile <x> -- sh -c "..."`.
- While unlocked, the derived vault key and decrypted secrets are resident in process memory (lock or quit to clear).

## Future work

See `docs/future-work.md` for deferred stable/post-stable work. Highlights:
OS keyring support, hardware-backed unlock, per-profile policy rules, provider
health checks, secure import/export, team mode, shell-plugin integration, full
IDE adapter coverage, automatic temp-env cleanup, richer TUI themes, audit
viewer filters, parser-backed existing user-scope TOML/XML merge, and signed
release provenance.

## Tests

```bash
go test ./...
go test -race ./...
go build -buildvcs=false ./...
go vet ./...
test -z "$(gofmt -l .)"
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run . adapter verify
```

Covers secret masking, encryption round-trip, wrong-password rejection, vault
CRUD, envelope validation (KDF bounds, nonce/salt format), needs-rekey
detection, provider validation (including env-var format, HTTPS enforcement, auth
type), profile store logic, adapter contract declarations and completeness,
contract enforcement (manual apps don't receive secrets), file-write safety
(redaction, backup, merge/overwrite refusal, atomic writes, symlink/scope
preflight), per-app adapter rendering, secret-propagation adversarial tests (raw
secret chased across TUI views, adapter renders, modals — must not appear), CLI
security contracts (no raw secret argv flags, profile resolution validation),
and integration.
