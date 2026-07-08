# Config App Cleanup Plan

This note describes how AegisKeys should handle applications that require
runtime config files, such as Crush, MiMo, OpenCode, Qwen Code, Goose, Hermes,
Vibe, and other config-file adapters.

## Goal

Launching a config-backed app must not leave raw API keys behind on disk, in
the parent shell, or in stale runtime config after the child process exits.

The desired stable-release contract is:

- Provider metadata may be written to app config files only when required.
- Raw API keys must never be written to app config files.
- API keys may be injected only into the child process environment.
- AegisKeys must restore or remove runtime config overlays after the child
  process exits.
- If cleanup fails, the CLI/TUI must report that failure clearly.

## Current Model

Adapters render `LaunchStrategy.Plan.Files` for apps that need config.
`runner.PrepareCommandWithCleanup` should:

1. Resolve and validate the launch strategy.
2. Snapshot each target config file before writing.
3. Apply the adapter-rendered file writes.
4. Launch the child process with sanitized, child-scoped environment variables.
5. Restore each preexisting config file after the child exits.
6. Remove any config file that AegisKeys created for the launch.

This keeps config writes as runtime overlays, not permanent ownership of the
user's app configuration.

## Required File Snapshot Semantics

Before writing any config file, record:

- expanded absolute path
- whether the file existed
- original file bytes
- original file mode

On cleanup:

- If the file existed before launch, restore the original bytes and mode.
- If the file did not exist before launch, remove the file.
- Cleanup should run in reverse write order.
- Cleanup should aggregate failures and return an error.

Do not rely on backup files as the primary restore mechanism. Backups are for
user recovery and auditability; snapshots are the runtime cleanup mechanism.

## Secret Handling Rules

Config-backed apps fall into two cases:

- Apps that support env-var references in config:
  - Config may include env var names such as `OPENROUTER_API_KEY`.
  - Config must not include raw secret values.
  - Runtime launch injects the actual secret into the child environment.

- Apps that do not support env-var references:
  - Prefer config with provider metadata only.
  - If the app cannot authenticate without a persistent raw key in config, mark
    the adapter as manual/keychain-backed instead of writing the key.

Local/no-auth providers should not receive fake env var references. Their config
entry should omit credential fields entirely.

## Provider Catalog Apps

Provider-catalog apps should write every compatible provider from
`providers.json`, not only providers that already have AegisKeys keys.

For each provider:

- Include provider name/slug/base URL/model metadata when supported by the app.
- Include credential env var names only when the app needs that metadata.
- Inject a child env secret only when AegisKeys has a launch-enabled key.
- Omit credential fields for local/no-auth providers.
- Never write raw secrets to catalog config.

This lets the app see the full configured provider catalog while keeping
AegisKeys-owned secrets scoped to the launched child process.

## Runner Contract

`runner.Run` and the TUI launch path must both use the cleanup-bearing
preparation path.

Required behavior:

- Preflight the command with `exec.LookPath` before writing config.
- Apply file writes only after launch validation passes.
- Always call cleanup after `cmd.Run` or `tea.ExecProcess` returns.
- Preserve child exit status, but include cleanup failure if cleanup failed.
- In the TUI, surface cleanup failure in `statusMsg`.

Known limitation: if the AegisKeys process is killed with `SIGKILL`, cleanup
cannot run. Stable release should document this and minimize the risk by never
writing raw secrets in the first place.

## Tests Needed

Filewriter tests:

- Created config file is removed after cleanup.
- Existing config file is restored byte-for-byte after cleanup.
- Original file mode is restored.
- Cleanup runs in reverse order.
- Cleanup failure is returned.
- Raw secret in file content is rejected.

Runner tests:

- `Run` restores config after successful child exit.
- `Run` restores config after non-zero child exit.
- `Run` reports cleanup failure.
- Missing command fails before config is written.
- Parent environment does not receive injected secrets.

TUI tests:

- TUI launch uses `PrepareCommandWithCleanup`.
- `launchFinishedMsg` calls cleanup.
- Cleanup failure is shown in status text.

Catalog adapter tests:

- Catalog config includes all compatible providers.
- Providers without AegisKeys keys still appear in config.
- Only providers with launch-enabled keys receive injected env secrets.
- Local/no-auth providers have no fake credential field.
- Config contains env var names, not raw API keys.

## Stable Release Checklist

- All config-writing adapters use the same cleanup-bearing runner path.
- No adapter writes raw API keys to config.
- No launch path calls bare config materialization without a restore hook.
- CLI and TUI both report cleanup failures.
- Golden snapshots prove catalog config contains metadata only.
- Smoke tests prove fake app launches do not leak secrets into config files,
  temp trees, parent env, argv, or preview text.
