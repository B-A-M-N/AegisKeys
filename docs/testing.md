# Testing

Run the core verification gates:

```bash
go test ./...
go test -race ./...
go build -buildvcs=false ./...
go vet ./...
test -z "$(gofmt -l .)"
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go run . adapter verify
```

## Coverage Map

The test suite covers:

- secret masking
- encryption round-trip
- wrong-password rejection
- vault CRUD
- envelope validation, including KDF bounds and nonce/salt format
- rekey and needs-rekey behavior
- provider validation, including env-var format, HTTPS enforcement, and auth
  type validation
- profile store behavior
- adapter contract declarations and completeness
- contract enforcement for manual/guided apps that must not receive secrets
- file-write safety, including redaction, backup, merge/overwrite refusal,
  atomic writes, and symlink/scope preflight
- per-app adapter rendering
- secret-propagation adversarial checks across TUI views, adapter renders, and
  modals
- CLI security contracts, including no raw secret argv flags and profile
  resolution validation
- integration paths for runtime injection and child-process execution

## Adapter Verification

`go run . adapter verify` validates render/file/no-leak behavior without
requiring target apps to be installed locally.

`go run . adapter verify --installed` additionally performs local CLI smoke
checks for installed target apps and is intended for maintainer machines, not
generic CI.
