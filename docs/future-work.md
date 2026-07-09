# Future Work

Deferred work for post-stable AegisKeys releases.

## Security And Unlock

- OS keyring integration for optional vault-key storage.
- Hardware-backed unlock support, such as YubiKey or passkey-assisted flows.
- Per-profile policy rules for export, launch, clipboard, and model/provider constraints.
- Secure import/export flows with encryption and explicit trust boundaries.

## Providers And Adapters

- Provider health checks that validate reachability without leaking secrets.
- Agent-specific launch presets for common command/model combinations.
- Full IDE adapter coverage where safe APIs exist; otherwise keep manual/keychain handoff explicit.
- Parser-backed TOML/XML merge/patch support for non-destructive existing config updates.

## Operations

- Automatic stale temporary env-file cleanup.
- Shell plugin integration for guided workflows that still avoid parent-shell secret export.
- Team mode with public-key sharing.
- Richer audit viewer filters.
- Richer TUI themes and accessibility polish.

## Release Engineering

- Signed release artifacts and provenance.
- Package-manager distribution, such as Homebrew, Arch, Nix, or Scoop.
- SBOM generation for release artifacts.
