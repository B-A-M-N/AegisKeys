# TIDYYING_UP.md

This is the final cleanup plan before tagging a public AegisKeys release. The
code is close to release-candidate quality; the remaining work is mostly
packaging hygiene, clean-environment proof, first-run verification, and sharp
edge review.

## Release Target

Ship a conservative `v0.1.0` that promises:

- Local encrypted vault storage.
- Provider metadata and secrets stored separately.
- Child-process-scoped credential injection.
- No raw secrets in adapter args, previews, generated config, audit logs, or
  ordinary CLI/TUI display.
- Verified adapters only where automated gates pass.
- Clear threat-model boundaries for malicious child tools and same-user process
  inspection.

Do not promise:

- Protection from malicious launched tools.
- OS-level same-user process isolation.
- Perfect memory zeroization in Go.
- Parser-backed TOML/XML merging for existing user config files.

## 1. Clean Repository Hygiene

Goal: remove local scratch/build artifacts before release.

Check:

```bash
git status --short
find . -maxdepth 1 -type f -perm -111 -print
find . -maxdepth 2 -type d \( -name dist -o -name .crush -o -name .gemini_security -o -name .aider.tags.cache.v4 \) -print
```

Review these current local artifacts before publishing:

- `aegiskeys-fixed`
- `knoxkeys`
- `dist/`
- `.crush/`
- `.gemini_security/`
- `.aider.chat.history.md`
- `.aider.tags.cache.v4/`
- `env`
- `printenv`

Expected action:

- Keep source, docs, tests, `go.mod`, `go.sum`, `.github/`, `LICENSE`,
  `SECURITY.md`, and `SECURITY_EVIDENCE.md`.
- Remove built binaries and local tool caches from the repository.
- Add ignore rules for generated binaries, `dist/`, local TUI/tool caches, and
  scratch files.

Suggested `.gitignore` additions:

```gitignore
/dist/
/aegiskeys
/aegiskeys-*
/knoxkeys
/.crush/
/.gemini_security/
/.aider.chat.history.md
/.aider.tags.cache.v4/
/env
/printenv
```

## 2. Clean-Clone Build Proof

Goal: prove the project builds without hidden local files.

From outside the repo:

```bash
tmp="$(mktemp -d)"
git clone /home/bamn/AegisKeys "$tmp/AegisKeys-clean"
cd "$tmp/AegisKeys-clean"
go test ./...
go build -buildvcs=false ./...
go vet ./...
gofmt -l .
```

Pass criteria:

- `go test ./...` passes.
- `go build -buildvcs=false ./...` passes.
- `go vet ./...` passes.
- `gofmt -l .` prints nothing.
- No dependency requires `/home/bamn/TUIs/*` or any other machine-local path.

If this fails:

- Fix `go.mod` first.
- Do not tag a release until a clean clone passes.

## 3. Race Test

Goal: catch obvious concurrency regressions in vault, runner, audit, and TUI
state handling.

Run:

```bash
go test -race ./...
```

Pass criteria:

- No race detector reports.
- No intermittent test failures.

If race test is too slow for routine CI, at least run it manually before each
release tag and document the result in `SECURITY_EVIDENCE.md`.

## 4. Fresh Config Smoke Test

Goal: verify the packaged CLI works from a blank config directory.

Build:

```bash
go build -buildvcs=false -o /tmp/aegiskeys-smoke .
cfg="$(mktemp -d)"
```

Run the smoke path:

```bash
/tmp/aegiskeys-smoke --config "$cfg" init
/tmp/aegiskeys-smoke --config "$cfg" provider list
/tmp/aegiskeys-smoke --config "$cfg" doctor --json
/tmp/aegiskeys-smoke --config "$cfg" completion bash >/tmp/aegiskeys-completion.bash
```

Then manually verify interactive flows:

```bash
/tmp/aegiskeys-smoke --config "$cfg" key add
/tmp/aegiskeys-smoke --config "$cfg" profile create
/tmp/aegiskeys-smoke --config "$cfg" env --profile <profile-name>
/tmp/aegiskeys-smoke --config "$cfg" run --profile <profile-name>
```

Pass criteria:

- First-run bootstrap is understandable.
- Key entry never appears in argv.
- `env` masks by default.
- `run` launches a child process with scoped env only.
- `doctor --json` emits parseable JSON.
- The generated config directory uses expected permissions:

```bash
find "$cfg" -maxdepth 2 -printf '%m %p\n' | sort
```

Expected:

- Directories: `0700`
- Sensitive files: `0600`

## 5. Release Binary Smoke Test

Goal: verify the actual release artifacts, not just `go run` or local build.

Build release artifacts:

```bash
make release VERSION=0.1.0-rc1
```

Verify:

```bash
sha256sum -c dist/SHA256SUMS
./dist/aegiskeys_0.1.0-rc1_linux_amd64 version
./dist/aegiskeys_0.1.0-rc1_linux_amd64 completion bash >/tmp/aegiskeys-release-completion.bash
```

Pass criteria:

- Checksums validate.
- Binary prints the expected version.
- Completion generation works.
- Binary can run the fresh config smoke test above.

## 6. README Quickstart Audit

Goal: make sure a new user can succeed by following only the README.

Review the README for:

- Correct install/build commands.
- No references to machine-local Charmbracelet replacement directories.
- Clear `init -> key add -> profile create -> env/run -> doctor` path.
- Clear warning near `run`: only launch commands you trust.
- Honest adapter status language.
- Honest backup/redaction language: config backups are best-effort redacted
  unless encrypted backup support is explicitly implemented.

Concrete check:

```bash
rg -n "/home/bamn|TUIs|replace|verified|manual_proof|re-save|rekey|malicious|trust" README.md SECURITY.md SECURITY_EVIDENCE.md
```

Fix any stale wording before release.

## 7. Sharp Edge Review

Goal: decide which dangerous-but-intentional commands are acceptable for
`v0.1.0`.

Review these commands:

- `aegiskeys key reveal`
- `aegiskeys vault reveal`
- `aegiskeys vault env`
- `aegiskeys env --export`
- `aegiskeys envfile`
- `aegiskeys run --profile ... -- <override>`

Required behavior:

- Commands that print raw secrets require explicit confirmation.
- Help text and warning text mention terminal scrollback/logging risk.
- Audit logs record metadata only.
- Arbitrary command override is blocked or clearly policy-gated when strict
  runtime policy is active.

If arbitrary command override remains enabled broadly, document this near the
`run` examples:

```text
Only launch commands you trust. AegisKeys scopes credentials to the child
process, but that child can still read and misuse its own environment.
```

## 8. Adapter Truthfulness Pass

Goal: make sure support badges and contracts tell the truth.

Run:

```bash
go test ./internal/adapter ./internal/runner ./internal/tui
```

Review:

- No adapter is `verified` unless all four gates are true.
- Manual/keychain-only apps are `guided` or `blocked`, not falsely
  `experimental`.
- `manual_proof` is used only when proof JSON exists.
- `testdata/adapter_proofs/*.proof.json` matches contracts.
- `testdata/adapter_golden/*.golden.json` is current.

If an adapter changes:

1. Update its contract.
2. Update or add proof JSON.
3. Update golden files only after manually inspecting the behavior.
4. Run adapter and runner tests again.

## 9. Doctor Output Review

Goal: make `doctor` useful for release support.

Run:

```bash
go run . doctor
go run . doctor --json
```

Check that doctor reports:

- Config permissions.
- Vault health.
- KDF/rekey status.
- Audit log status.
- Adapter proof/verification status.
- Any stale plaintext env files.

Doctor fix text must say `aegiskeys vault rekey` for KDF upgrades. It must not
say ordinary unlock/re-save upgrades KDF params.

## 10. CI Readiness

Goal: make GitHub Actions useful on the first push.

Check:

```bash
sed -n '1,220p' .github/workflows/ci.yml
```

CI should run:

- `go test ./...`
- `go build -buildvcs=false ./...`
- `go vet ./...`
- `gofmt -l .`

Optional but recommended:

- Add a separate manual or scheduled race-test job.
- Add release artifact build on tags.

## 11. Tagging Checklist

Before tagging:

```bash
go test ./...
go test -race ./...
go build -buildvcs=false ./...
go vet ./...
test -z "$(gofmt -l .)"
make release VERSION=0.1.0
sha256sum -c dist/SHA256SUMS
```

Then:

```bash
git status --short
git tag -a v0.1.0 -m "v0.1.0"
```

Only tag if:

- The worktree contains no accidental local artifacts.
- Clean-clone proof passed.
- Fresh-config smoke passed.
- Release binary smoke passed.
- README and security docs match the actual behavior.

## Known Acceptable Gaps For v0.1.0

These are acceptable if documented clearly:

- Same-user process isolation is OS-dependent and out of scope.
- Go memory zeroization is best-effort.
- Existing TOML/XML user config merge is fail-closed until parser-backed merge
  exists.
- A malicious child process can exfiltrate credentials it is intentionally
  given.
- Config backup redaction is best-effort unless encrypted backup support is
  added.

## Post-v0.1.0 Follow-Ups

- OS keyring integration for vault-key storage.
- Parser-backed TOML/XML managed-block merge.
- Encrypted config backups for files likely to contain credentials.
- More platform-specific doctor checks.
- VHS/demo recordings.
- Signed release artifacts.
- Package-manager distribution.
