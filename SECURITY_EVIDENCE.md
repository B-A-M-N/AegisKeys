# SECURITY_EVIDENCE.md — AegisKeys Security Model Proof

This document records the executable evidence for each README security claim.
Tests are adversarial: they attempt to violate the claim and assert the system resists.

## Claims and Evidence

### 1. Secrets are encrypted at rest

| Claim | Test | Result |
|-------|------|--------|
| No raw key in `vault.enc` | `secret.TestSecretNeverSerialized` | PASS |
| Wrong password fails closed | `secret.TestVaultRoundTrip` | PASS |
| Tampered ciphertext fails closed | `secret.TestTamperedCiphertextFails`, `TestVaultEnvelope_TamperedCiphertext` | PASS |
| Malformed envelope cannot panic/abuse | `secret.TestVaultFuzz_Envelope` (13 mutations) | PASS |
| Argon2 resource exhaustion prevented | `runner.TestArgon2_ResourceExhaustion` | PASS (1 GiB rejected before IDKey) |

### 2. Provider metadata and key material stay separated

| Claim | Test | Result |
|-------|------|--------|
| `providers.json` never contains API keys | `provider.TestValidateStrict_*` | PASS |
| `profiles.json` only references key IDs | model invariant | PASS by construction |
| Profile env cannot become plaintext secret store | `adapter.TestProfileEnvBypass_Attempts` (7 patterns) | PASS |

### 3. Injection is child-process-scoped

| Claim | Test | Result |
|-------|------|--------|
| Child receives intended secret | `runner.TestRuntimeInjection_RealRunner` (real runner.Run) | PASS |
| Parent env unchanged | same test | PASS |
| Non-credential parent secrets do not leak to child | `runner.TestPrepareCommand_AppliesEnvAllowlist`, `runner.TestBuildChildEnv_StripsNonCredentialVarSecrets` | PASS |
| Config files don't contain raw secrets | `adapter.TestFileWriter_RefusesRawSecretInConfigFile` | PASS |
| TUI launch uses shared runner preparation | `tui` launch tests + `runner.PrepareCommand` path | PASS |

### 4. AegisKeys does not lie about unsupported apps

| Claim | Test | Result |
|-------|------|--------|
| `CanInjectSecrets=false` => no secret in plan | `adapter.TestContractEnforcement_PerAdapter` | PASS |
| `Blocked=true` => runner refuses | `runner.TestBlockedStrategy_ActualExecution` | PASS |
| Manual apps never get raw secrets | `adapter.TestContractEnforcement_ManualAppNoSecret` | PASS |

### 5. Every output surface is redaction-safe

| Claim | Test | Result |
|-------|------|--------|
| TUI views | `tui.TestSecretPropagation_TUIVIEW` | PASS |
| Adapter previews | `tui.TestSecretPropagation_ADAPTERRENDER` | PASS |
| Modals | `tui.TestSecretPropagation_MODALS` | PASS |
| Masked keys | `adapter.TestRedactionSurface` | PASS |
| Error strings | `runner.TestErrorPath_Leak` | PASS |
| CLI add commands do not accept raw secret argv flags | `cmd.TestSecretAddCommandsDoNotAcceptSecretFlags` | PASS |
| Provider inspect/list/search/validate/export output is redacted | `cmd.TestRedactProviderOutput`, `cmd.TestValidateProviderRegistryForExportRefusesSecrets` | PASS |
| Audit log fields are pattern-redacted before write | `audit.TestLoggerRedactsSecretLookingMetadata` | PASS |
| Doctor JSON emits structured severity/message/fix fields | `cmd.TestBuildDoctorOutputJSONShape` | PASS |

### 6. Concurrent access

| Claim | Test | Result |
|-------|------|--------|
| No Go data races | `go test -race` | PASS |
| Vault not corrupted | `secret.TestVault_Concurrent_AddSaveLoad` | PASS |
| No data loss | `runner.TestConcurrentSave_Durability` | PASS |

### 6.1 Dependency vulnerabilities

| Claim | Test | Result |
|-------|------|--------|
| No reachable known vulnerabilities in code or standard library | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` with Go 1.25.12+ | PASS |

### 7. Saved profiles resolve before use

| Claim | Test | Result |
|-------|------|--------|
| Key/provider mismatch rejected | `cmd.TestValidateResolutionRejectsKeyProviderMismatch` | PASS |
| Unknown target app rejected at save-time validation | `cmd.TestValidateResolutionRejectsUnknownGenericTargetAtSaveTime` | PASS |

### 8. Config writing is honest about unsupported merge modes

| Claim | Test | Result |
|-------|------|--------|
| Existing user TOML is not clobbered | `adapter.TestApplyFileWrites_TOMLRefusesExistingUserConfig` | PASS |
| Existing user XML is not clobbered | `adapter.TestApplyFileWrites_XMLRefusesExistingUserConfig` | PASS |
| Fresh TOML config can be written | `adapter.TestApplyFileWrites_TOMLAllowsFreshUserConfig` | PASS |
| Audit log is created with locked-down permissions | `audit.TestLoggerCreatesParentAndLocksPermissions`, `audit.TestLoggerRepairsPermissiveAuditLog` | PASS |

### 9. Adapter verification is auditable

| Claim | Test | Result |
|-------|------|--------|
| Verified adapters have all four gates true | `adapter.TestAdapterTruthTableVerifiedAdapters` | PASS |
| Render output matches golden snapshots | `adapter.TestAdapterVerificationGates` | PASS |
| Adapter output does not leak raw secret to args/preview/files | `adapter.TestAdapterVerificationGates` | PASS |
| Config writes merge/apply without secret leakage | `adapter.TestAdapterVerificationGates` | PASS |
| Launch smoke uses fake executables, no network/API calls | `runner.TestAdapterFakeExecutableLaunchSmoke` | PASS |
| Child exit code is preserved | `runner.TestAdapterFakeExecutableLaunchSmokeExitCodePreserved` | PASS |
| Provider-catalog adapters have golden snapshots and config no-secret checks | `adapter.TestCatalogVerificationGoldens` | PASS |
| Provider-catalog launch path reaches fake Crush/MiMo/OpenCode executables without config leaks | `runner.TestCatalogAdapterFakeExecutableLaunchSmoke` | PASS |
| CLI adapter verification does not require installed third-party CLIs by default | `cmd.TestAdapterVerifyDefaultDoesNotRequireInstalledCLI`, `aegiskeys adapter verify` | PASS |

## Discovered and fixed during testing

| Gap | Fix |
|-----|-----|
| Runner accepted blocked strategies (RunConfig lacked Blocked field) | Added Blocked/BlockReason to RunConfig; Run() refuses |
| ResolveRunConfig discarded Blocked metadata | Switched to ResolveLaunchStrategy |
| Runner's Blocked enforcement missing | Added explicit refusal before exec.Command |
| Concurrent SaveVault lost data | Added cross-process flock and merge-on-disk preservation |
| Raw secret argv flags exposed key material | Removed `key add --secret` and `vault add --secret`; prompt only |
| CLI profile create could save broken profiles | Added central `resolve.ValidateResolution` and render-mode derivation |
| TOML/XML “merge” could overwrite existing user config | Refuse existing user/project overwrite until parser-backed merge exists |
| Provider metadata commands could display/export corrupted secret-bearing metadata | Redact provider CLI output; refuse export unless strict metadata validation passes |
| Audit logger trusted all future metadata callers | Pattern-redact audit event fields before write and force `0600` on the audit file |
| `adapter verify` default mode depended on locally installed target apps | Split render/files/no-leak verification from optional `--installed` smoke checks |

## Honest remaining gaps

| Gap | Risk | Mitigation needed |
|-----|------|-------------------|
| Sibling process isolation not explicit | Low | Out of scope (OS property) |
| Memory zeroization best-effort | Low | Out of scope (Go GC) |
| Existing user-scope TOML/XML merge still limited | Medium | Fail closed until parser-backed managed-block merge is complete |

## Central enforcement gate

```
adapter.ValidateLaunchStrategy(strategy, prof, prov, key, policy)
```

Called by: `ResolveLaunchStrategy`, `ResolveRunConfig`, profile save validation,
`runner.PrepareCommand`, and `runner.Run`.

## Honest scope boundary

**Protects against:** accidental leakage to terminal/logs/commits, plaintext persistence in config, broad shell env exposure, unsafe file permissions, manual apps receiving secrets, malformed vault resource abuse.

**Does NOT protect against:** child exfiltration (by design), same-user process inspection, concurrent profile edits clobbering, malware/root/kernel/keyloggers, shoulder surfing.
