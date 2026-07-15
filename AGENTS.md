# AGENTS.md — AegisKeys

A local-first, secure terminal app (Go 1.25.12+) that stores API **provider** metadata and **secrets** separately, then injects the correct credentials into coding agents/CLIs. AegisKeys renders **app-specific launch configuration** — env vars, CLI args, and config files — so coding agents get credentials in the exact format each one expects.

> Canonical specs: `SPEC.md` (full product spec, CLI surface, storage layout, threat model) and `TUI_GUIDE.md` (Charm v2 TUI architecture, screen-by-screen, pitfalls). Read those before non-trivial work.

---

## Build Status (read first)

As of the latest build:
- `go build -buildvcs=false ./...` passes (VCS stamping errors unless `-buildvcs=false` is set, due to .git metadata in this checkout).
- `go test ./...` passes across all packages.
- `go vet ./...` is clean.
- `gofmt -l .` is empty.
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` reports no reachable vulnerabilities when run with Go 1.25.12+.
- Full CLI (Cobra) with all SPEC §18 commands implemented and tested end-to-end.
- An interactive TUI (bubbletea v2) with password-unlock, 9 screens, multi-field add forms with provider/key selection, model-slot collection, and real child-process launch via `tea.ExecProcess`.
- **Adapter system** (`internal/adapter`) — per-app renderers implementing `AppAdapter` contract interface with `AppSupportContract` metadata for 21 targets: Generic, Crush, Aider, Cline, Hermes, Qwen Code, Goose, Claude Code, Mistral Vibe, Codex, MiMo, OpenCode, OpenHands, Gemini CLI, Copilot CLI, Continue, Roo Code, Kilo Code, Cursor, Zed, IntelliJ.
- **Provider protocol/model catalog** (`internal/provider`) — rich provider metadata with auth spec, endpoints, model catalog, capabilities, app hints.
- **Proxy support** (`internal/proxy`) — auto-start local proxies (SOCKS5/HTTP) when apps need them.
- **Model slots** — profiles support per-app model roles including feature-specific slots (compression/vision/web_extract for Hermes; inline_assistant/subagent/commit_message/thread_summary/alternatives for Zed; catalog/fallback slots for catalog-driven CLIs).
- **Config file rendering** with merge/backup/redaction semantics (`internal/adapter/filewriter.go`); JSON/JSONC/YAML/TOML use parser-backed merges and XML uses an identity-aware structural patch, preserving unrelated entries in existing user/project config.
- **Hard boundary enforcement**: `ValidateLaunchStrategyForMode` is the mandatory gate. It calls `ValidateContract` to ensure adapter contracts are fully and honestly declared before any contract field is trusted, then checks raw-secret-leak, profile-env-override, and blocked-strategy invariants. `ResolveLaunchStrategy` calls it for `ResolveRun`; `ResolveLaunchStrategyCatalog` calls it for preview/run/save modes; `ResolveRunConfig` calls `ValidateLaunchStrategy` separately as a second gate; profile save validation and TUI launch both revalidate.
- **Adapter contracts strengthened**: `ValidateContract` requires `ConfigFiles` when `CanPatchConfig=true`, `ValidationChecks` when `verified`, `DisplayName`, `DefaultCommand` when `CanLaunch=true`, `Fix` on high/critical hazards, and known enum values for support/confidence/surface/render-mode fields.
- **Adapter confidence truthfulness**: `manual_proof` is distinct from `verified`; Generic, Crush, Aider, Qwen Code, Goose, and Claude Code are `verified` with proof JSON under `testdata/adapter_proofs/`, render goldens under `testdata/adapter_golden/`, and all four automated gates true. Catalog adapters (Crush, MiMo, OpenCode) have catalog golden snapshots and config no-secret checks.
- **Adapter proof doctor**: `doctor` reports adapter confidence/proof status, fails falsely verified adapters, and warns if repo-local manual-proof files are missing when `testdata/adapter_proofs/` is discoverable.
- **Provider HTTPS enforcement**: `ValidateStrict` rejects non-https base URLs for non-local providers (loopback exempted), including CLI `provider add/edit/validate`.
- **Secret argv protection**: `key add`, `vault add`, `key rotate`, and `vault reveal` never accept raw secret flags; secrets are read through no-echo prompts. The sole exception is `init --password` for non-interactive automation (flagged as less secure). Launch validation rejects raw-secret substrings in argv, preview, config file content, and non-injecting env plans.
- **Vault overwrite protection**: `SaveVault`/`SaveVaultWithKey` refuse to overwrite an existing vault if the password/session key cannot reopen the on-disk envelope.
- **Filewriter symlink protection**: `rejectSymlinkParents` walks every parent directory; `expandPath` only expands HOME/XDG_CONFIG_HOME/TMPDIR (no ambient env injection).
- **Secret cleanup**: `lockVault` runs on every quit path (`ctrl+c`, `q`) to zero the derived key.
- **Wizard** — app-first profile creation with real model-slot input collection and `wizardCanAdvance` validation.
- Unit tests for all packages: `internal/{secret,provider,profile,adapter,proxy,runner,redact,security,tui,config,audit}`.
- **Env allowlist enforced on all launch paths**: both `Run()` (strategy-driven) and `RunLegacy()` filter parent env through `baseEnvForClass` (CLI vs GUI/IDE) before `BuildChildEnv`, stripping non-allowlisted and secret-looking vars so unrelated parent secrets do not leak into child processes.

### Architecture

**Three-domain model + adapter layer:**

| Domain | Package | On Disk | Secret? |
|--------|---------|---------|---------|
| **Providers** (metadata) | `internal/provider` | `providers.json` | No |
| **Secrets** (API keys) | `internal/secret` | `vault.enc` (Argon2id → AES-256-GCM) | Yes |
| **Profiles** (binding) | `internal/profile` | `profiles.json` | No (refs key id) |
| **Adapter** (rendering) | `internal/adapter` | — | No |
| **Protocol bridge** (Anthropic↔OpenAI) | `internal/bridge` | — | In-memory only |
| **Proxy** (tunneling) | `internal/proxy` | — | No |
| **Logo masks** (TUI visuals) | `internal/logo` | `assets/logos/*.png` | No |
| **Runner** (execution) | `internal/runner` | — | No |

**Profile → Launch Plan flow:**
```
Profile (provider + key + models + target app)
  → ResolveLaunchPlan(profile, provider, key, adapterRegistry)
    → adapter.Render() → LaunchPlan{Command, Args, Env, Files}
      → runner.PrepareCommand()/runner.Run() writes files, builds sanitized env, launches child
```

**Provider model:**
- `Protocol` — openai / anthropic / google / local
- `AuthSpec` — bearer/header/query/none + env var
- `EndpointSpec` — base URL, API path, models URL
- `ModelCatalogSpec` — static/dynamic/manual/local + refresh config
- `Capabilities` — tool_use, vision, streaming, function_calling
- `AppHints` — per-application config hints

**Profile model:**
- `Target` — app, render mode, command override
- `Models` — ModelSlots{Main, Fast, Weak, Editor, Planner, Actor, Subagent, Catalog, Fallbacks}
- `Files` — TargetConfigFile{Path, Format, Content}
- `Env`, `Args` — user overrides

**Per-app model slots:**
| App | Slots |
|-----|-------|
| Generic OpenAI-compatible | main |
| Aider | main, weak, editor |
| Cline | planner, actor |
| Goose | main, fast |
| Hermes | main, compression, vision, web_extract |
| Claude Code | main, fast, planner, subagent |
| Qwen Code | main, catalog, fallbacks |
| Crush | main, catalog |
| Mistral Vibe | main |
| Codex | main, gpt54, gpt54mini, gpt53codex, gpt52codex, gpt52, gpt51codexmax, gpt51codexmini |
| MiMo | main |
| OpenCode | main |
| OpenHands | main |
| Gemini CLI | main |
| Copilot CLI | main |
| Continue | main |
| Roo Code | main |
| Kilo Code | main |
| Cursor | none |
| Zed | main, inline_assistant, subagent, commit_message, thread_summary, alternatives |
| IntelliJ | main |

**Per-app config files:**
- Qwen Code → `~/.qwen/settings.json` (modelProviders)
- Cline → `~/.cline/data/settings/providers.json`
- Goose → `~/.config/goose/config.yaml`
- Hermes → `~/.hermes/config.yaml`
- Vibe → `~/.vibe/config.toml`
- Crush → `~/.config/crush/crush.json`

### Commands

```bash
go build -buildvcs=false ./...   # passes
go test ./...      # runs all tests
go vet ./...       # clean
gofmt -l .         # empty
```

Ensure `gofmt -l .` is empty.

Module name is **`aegiskeys`** (lowercase, single word). Intended CLI surface (SPEC §18, **implemented**):

```
aegiskeys init | tui | version | lock | unlock | doctor | audit
aegiskeys provider {list|add|inspect|models|refresh-models|remove|edit|search|validate|export}
aegiskeys key {add|list|show|rename|rotate|delete|reveal}
aegiskeys vault {add|list|show|copy|reveal|env|rename|rotate|archive|link|unlink|delete|inspect|backup|repair-unlock|rekey}
aegiskeys profile {create|list|inspect|delete}
aegiskeys run --profile <name> -- <command>
aegiskeys env --profile <name> [--export]
aegiskeys envfile --profile <name> | shred-envfile <path>
aegiskeys handoff --profile <name>
aegiskeys settings {show|set|reset}
aegiskeys adapter verify [--app <id>] [--installed]
aegiskeys completion {bash|zsh|fish|powershell}
```

### Dependency Portability

`go.mod` must remain portable: do not commit machine-local `replace` directives for the Charm v2 stack. Use `charm.land/.../v2` import paths, never `github.com/charmbracelet/...`.

### Release Runbook

Use `docs/release.md` as the maintainer checklist for public release gates,
artifact builds, tag workflow, and release-boundary language.

---

## Architecture

Three-domain model, kept deliberately separated (SPEC §3):

| Domain | Package | On-disk | Secret? |
|--------|---------|---------|---------|
| **Providers** (metadata) | `internal/provider` | `providers.json` | No |
| **Secrets** (API keys) | `internal/secret` | `vault.enc` (encrypted) | Yes |
| **Profiles** (binding) | `internal/profile` | `profiles.json` | No (references key id) |

Plus: `internal/config` (paths + app config), `internal/runner` (child-process env injection), `internal/audit` (append-only JSONL log), `internal/security` (doctor checks + output redaction), `internal/logo` (approved logo asset → terminal mask sampling for TUI matrix-rain silhouettes), `internal/app` (placeholder top-level struct, not yet wired), `internal/tui` (Charm v2 UI).

### Config directory layout (SPEC §7)

```
~/.config/aegiskeys/
├── config.json     0600
├── providers.json  0600
├── profiles.json   0600
├── vault.enc       0600   (Argon2id-derived key → AES-256-GCM)
├── audit.log       0600
└── tmp/            0700   (temp env files, 0600)
```

`DefaultConfigDir()` → `~/.config/aegiskeys`; `EnsureDir` mkdirs with `0700`.

### Data flow (target)

`provider.Registry` → `[]Provider`; `secret.Vault` → `[]MaskedKeyItem`; `profile.Store` → `[]Profile`. While unlocked, the TUI root model owns a vault session containing decrypted secrets and a derived key so it can add/rotate/delete/launch without retaining the master password. Child views render masked data only; `lockVault` zeroes the derived key and clears decrypted secret strings.

---

## Conventions observed in current code

- **Load functions are first-run friendly**: `LoadRegistry`, `LoadStore`, `LoadConfig` return a sensible default (`NewRegistry()`, `NewStore()`, `DefaultConfig()`) when the file is missing, only erroring on malformed JSON. Preserve this — do not turn missing-file into a hard error.
- **File perms enforced at write time**: every save uses `0600`; dirs use `0700`. Never write config/vault/profile/audit files looser.
- **Time fields set on mutate**: `Add`/`Save` set `CreatedAt`+`UpdatedAt` to `time.Now()`. `SaveStore` re-stamps `UpdatedAt` on every save (note: it overwrites all timestamps on every write).
- **Uniqueness enforced**: provider slug (`Registry.Add`), profile name (`Store.Add`). Aliases must not collide (`Store.Validate`).
- **JSON indent is inconsistent** today: config uses 1-space indent, registry/profile use 2-space. Match whatever the file you're editing already uses.
- **IDs**: secret records use `key_` + 8 random hex bytes (`secret.NewID`).
- **Provider IDs vs slugs**: `Provider` has both `ID` and `Slug`; defaults set them equal. Slugs are the user-facing key.

---

## Security invariants (do not violate)

These are the project's core contracts (SPEC §6, §10). Violating any of them is a critical defect:

1. **Never serialize the raw secret.** `SecretRecord.Secret` carries `json:"-"`. `Vault.Serialize()` rebuilds a Safe struct that omits it. When adding fields to `SecretRecord`, never give the `Secret` field a real JSON tag.
2. **Display only masked values.** Everything shown to the user goes through `MaskedKeyItem` and `MaskSecret()` (`...91ef` style — only last 4 chars shown; secrets ≤8 chars → `<hidden>`). The TUI root may hold a decrypted vault session while unlocked for mutations/launch, but rendered views and child screen state must not expose raw secrets.
3. **Redact all output** that might contain secrets via `security.Redact()` (bearer tokens, `api_key=`/`token=`/`secret=`/`password=`, and long `UPPER=value` env assignments) and `runner.BuildEnvString(..., redact=true)`. There is also `secret.RedactEnv`.
4. **Audit log contains metadata only** — never key values. `audit.Event` has no secret field by design.
5. **Child-process-scoped injection only.** `runner.Run` builds the child env and never exports to the parent shell. Preserve exit codes (it already does via `syscall.WaitStatus`).
6. **Fail closed.** Decryption errors return wrapped errors (`OpenEnvelope` → "decryption failed (wrong password?)").

### Known design detail in crypto (was a latent bug, now fixed)

`internal/secret/crypto.go` `SealEnvelope` uses `block.Seal(nil, decodeB64(nonce), []byte(plaintext), nil)` — args in the correct `Seal(dst, nonce, plaintext, ad)` order. If you ever see this reverted to `Seal(nil, plaintext, nonce, ...)`, it's a bug (nonce/plaintext swapped).

---

## TUI notes (from TUI_GUIDE.md)

- Stack: Bubble Tea v2 + Bubbles + Lip Gloss + Huh + Glamour, optional Fang over Cobra. All `charm.land/.../v2`.
- Palette names in `styles.go`: Vault Gold (`214`), Iron Gray (`240`), Muted Blue (`39`), Signal Green (`46`), Warning Amber (`214`), Danger Red (`196`).
- **Async rule**: all blocking I/O (vault unlock, doctor) must go through `tea.Cmd` returning a message — never call services synchronously in `Update`.
- **TUI delegates business logic** to `internal/*`; TUI may orchestrate commands/messages but must use adapter/runner/secret/profile services for validation, launch prep, and persistence.
- Status is never color-only: always include text (`✓ OK`, `! WARN`, `✗ FAIL`).
- Child-process launches exit/suspend the TUI, run the child, then resume — do not run the child inside a viewport.
- Global keys: `Tab`/`Shift+Tab` switch screens, `1`–`8` jump, `Ctrl+L` lock vault, `q`/`Ctrl+C` quit from dashboard (else back).

---

## Suggested next steps (post-MVP)

These are now implemented; remaining work is polish and future features:

- Add platform-specific doctor checks such as shell history scanning.
- Maintain `docs/future-work.md` as stable/post-stable deferred work changes.
- Extend VHS demo coverage as new TUI flows stabilize.
