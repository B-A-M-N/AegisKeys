# Release Runbook

This runbook describes the public release path for AegisKeys.

## Requirements

Use `docs/release-evidence.md` for the per-release commit/host/command/result record.

- Go 1.25.13 or newer.
- A clean git worktree.
- GitHub Actions enabled for this repository.
- Tag names use `vMAJOR.MINOR.PATCH`, for example `v0.1.0`.

## Local Preflight

Run the same gates used by CI before tagging:

```bash
test -z "$(gofmt -l .)"
go build -buildvcs=false ./...
go test ./...
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
go run . adapter verify
```

The CI workflow additionally sets `AEGISKEYS_ALLOW_TCP_TESTS=1` for bridge/provider
TCP integration tests and `AEGISKEYS_RUN_SOCKET_E2E=1` for broker Unix-socket E2E.
Sandboxes that deny `socket(2)` report explicit skips; release evidence still
requires the network/socket-enabled CI job to pass.

`adapter verify` checks render/file/no-leak behavior without requiring target
apps to be installed. Use `go run . adapter verify --installed` only for a
maintainer machine that intentionally has the supported CLIs on `PATH`.

## Release Artifact Smoke

Use `make release` to build the distributable binaries and checksums:

```bash
make release VERSION=0.1.0
sha256sum -c dist/SHA256SUMS
```

The release target builds:

- `linux_amd64`
- `linux_arm64`
- `darwin_amd64`
- `darwin_arm64`

## Publish

1. Confirm `README.md`, `SECURITY_EVIDENCE.md`, and `docs/future-work.md` are
   current.
2. Commit all release-ready changes.
3. Configure the repository variable `RELEASE_SIGNING_FINGERPRINT` to the
   trusted full GPG signer fingerprint. The workflow verifies both the tag
   signature and this exact signer before building; missing configuration or a
   different signer fails closed.

4. Create and push a signed tag:

   ```bash
   git tag -s v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

5. The release workflow builds artifacts, runs the full gate set, verifies
   `dist/SHA256SUMS`, creates build provenance attestations, uploads artifacts,
   and publishes a GitHub release.
6. After the workflow completes, download one release binary and run:

   ```bash
   ./aegiskeys version
   ./aegiskeys adapter verify
   ```

## Manual Workflow Dispatch

For dry runs without creating a tag, use the `Release` workflow's manual
dispatch and provide the version without a leading `v`. Manual dispatch uploads
artifacts but does not publish a GitHub release.

## Release Boundary

Stable support covers the secure vault core, CLI/TUI workflows, and adapters
marked `verified`. Experimental and guided adapters are shipped for convenience
but retain their lower confidence labels until their contracts have real launch
proof and all verification gates pass.

The CLI/TUI is built for Linux, macOS, and Windows. The credential broker is
Linux-only because peer-authenticated executable identity is implemented with
Linux `SO_PEERCRED`; other platforms fail closed for broker serving. A beta
release must capture the exact commit, host, Go version, command, and result for
every gate below. `govulncheck` must use a pinned version and a network-enabled
host. Adapter `verified` status means automated render/file/no-leak gates have
passed; it does not by itself prove every currently released third-party app
accepts the rendered configuration. A versioned real-launch qualification
record is required before calling that evidence complete.

The release uses a per-asset logo review manifest. The internal generic identity is the only publication-safe embedded asset in the current build; unresolved dedicated third-party illustrations remain source-tree review artifacts and resolve to the generic identity at runtime. Third-party names identify compatibility only and do not imply endorsement. Dedicated artwork requires an individual review record before it is embedded.
