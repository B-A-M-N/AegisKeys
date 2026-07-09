# Release Runbook

This runbook describes the public release path for AegisKeys.

## Requirements

- Go 1.25.12 or newer.
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
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run . adapter verify
```

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
3. Create and push a signed tag:

   ```bash
   git tag -s v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

4. The release workflow builds artifacts, runs the full gate set, creates build
   provenance attestations, uploads artifacts, and publishes a GitHub release.
5. After the workflow completes, download one release binary and run:

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
