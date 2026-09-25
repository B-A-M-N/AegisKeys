# Release Evidence Record

Complete this record on the release host. Do not copy historical PASS labels.

- Commit: `08b4906f12c5d18738ba5888f9014cca843e3dd3` plus release commit
- Date/UTC: 2026-09-24 / pending release time
- Host/OS/arch: Linux 7.0.11-76070011-generic / x86_64
- Go version: go1.25.13 linux/amd64
- Public-beta version: 0.1.0 local artifact rehearsal

| Gate | Command | Result/evidence |
|------|---------|-----------------|
| Format | `test -z "$(gofmt -l .)"` | PASS |
| Build | `go build -buildvcs=false ./...` | PASS |
| Unit/integration | `go test ./...` | PASS (TCP groups explicit sandbox skip; rerun below) |
| TCP integration | `AEGISKEYS_ALLOW_TCP_TESTS=1 go test ./internal/bridge ./internal/provider` | PASS |
| Broker sockets | `AEGISKEYS_RUN_SOCKET_E2E=1 go test ./internal/broker -run 'TestBrokerUnixSocketE2E|TestBrokerConcurrentResolveRotateImmediateRevocation' -count=1` | PASS |
| Race | `go test -race ./...` | PASS |
| Vet | `go vet ./...` | PASS |
| Vulnerabilities | `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` | PASS: 0 reachable vulnerabilities; 4 non-reachable module findings reported by the pinned scanner |
| Embedded assets | `go run . assets-check` | PASS: publication-safe generic asset plus generic fallback for 27 identities |
| Adapter contracts | `go run . adapter verify` | PASS with expected manual/blocked skips; synthetic verification only |
| Installed app smoke | `go run . adapter verify --installed` | PENDING: not rerun; real third-party qualification remains separate from synthetic adapter evidence |
| Broker benchmark | `go test ./internal/broker -run '^$' -bench BenchmarkConcurrentResolveRotateImmediateRevocation -benchtime=1x` | PENDING: rerun on release host |
| Artifacts/checksums | `make release VERSION=0.1.0` then `sha256sum -c dist/SHA256SUMS` | PASS on this host: linux/darwin amd64/arm64 |
| Installed binary | run `version`, `assets-check`, and `adapter verify` from built Linux artifact | PASS: version, assets-check, synthetic adapter verify |
| Real-app qualifications | records under `testdata/adapter_qualification/` | PENDING: synthetic verification is not real-app qualification |
| Logo provenance/license | per-asset manifest + per-asset review disposition | PASS for generic fallback policy; dedicated assets require individual affirmative review |

## Post-audit spot-check remediation (2026-09-24)

- Approval recovery now reconciles only affected secret records, combining
  administrative/manual baselines with currently enabled grant requirements.
- Regression coverage passed for unrelated policy, captured administrative
  baseline, concurrent administrative change, concurrent enabled grant, and
  direct grant cleanup.
- External editor preparation carries session generation + scratchpad ID;
  stale plaintext is deleted before editor launch.
- Active TUI vault snapshots are replaced through one zeroizing helper; a
  source audit found no remaining direct `vaultSession.vault =` assignments.
- Stale wizard fetch results no longer clear the current loading indicator.
- Full tests and race tests were completed on the release host; refresh this record after the final commit.

## Latest source-audit remediation (2026-09-24)

Verified after implementation:

- `go test ./...` — PASS
- `go test -race ./...` — PASS
- `go build -buildvcs=false ./...` — PASS
- `go vet ./...` — PASS
- `gofmt` / `git diff --check` — PASS
- `govulncheck@v1.6.0` — PASS: 0 reachable vulnerabilities
- `go run . assets-check` — PASS: publication-safe generic asset plus generic fallback for 27 identities
- `go run . adapter verify` — PASS with expected manual/blocked skips; this is synthetic evidence
- Logo distribution — per-asset gate active; unresolved dedicated assets use the generic runtime fallback

The remediation adds transactional overlay rollback, broker lock completion
barriers, proxy identity/log containment, provider catalog origin and size
limits, single-flight TUI unlock fencing, Argon2 resource ceilings across
recovery/rekey paths, and randomized private env-file construction.
