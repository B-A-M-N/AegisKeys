# Future Work

Deferred work for post-stable AegisKeys releases.

## Security And Unlock

- Hardware-backed unlock support, such as YubiKey or passkey-assisted flows.
- Per-profile policy rules for export, launch, clipboard, and model/provider constraints.
- Secure import/export flows with encryption and explicit trust boundaries.

## Providers And Adapters

- Provider health checks that validate reachability without leaking secrets.
- Agent-specific launch presets for common command/model combinations.
- Full IDE adapter coverage where safe APIs exist; otherwise keep manual/keychain handoff explicit.

## Operations

- Stronger broker-supervised application identity (dedicated launchers or OS-user/sandbox separation) for interpreter-hosted applications.
- Broker running-state controls in the TUI beyond the current status display and start command.
- Optional broker integration examples for additional language ecosystems.
- Hash-only grant workflows with signed update/rebuild flows.
- Automatic stale temporary env-file cleanup.
- Shell plugin integration for guided workflows that still avoid parent-shell secret export.
- Team mode with public-key sharing.
- Richer audit viewer filters.
- Richer TUI themes and accessibility polish.

## Release Engineering

- Signed release artifacts and provenance.
- Package-manager distribution, such as Homebrew, Arch, Nix, or Scoop.
- SBOM generation for release artifacts.
- A recorded real-application qualification matrix for every adapter that may claim verified support; fake-executable smoke tests remain contract tests, not app compatibility proof.
