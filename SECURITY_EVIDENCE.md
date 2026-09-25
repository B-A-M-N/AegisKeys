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

### 6.1 Credential broker and transactional vault

| Claim | Test | Result |
|-------|------|--------|
| Delete persists and is not resurrected | `secret.TestMutateVault_DeletePersists` | PASS |
| Concurrent independent rotations survive | `secret.TestMutateVault_IndependentExistingRecordUpdatesBothSurvive` | PASS |
| Concurrent add/delete both survive | `secret.TestMutateVault_ConcurrentAddAndDeleteBothSurvive` | PASS |
| Zeroization clears extra secret components and scratchpads | `secret.TestZeroVaultClearsAllSecretComponents` | PASS |
| Unsafe socket path is refused | `broker.TestListenRejectsUnsafePreexistingPath` | PASS |
| Broker metadata is 0600 and contains no credential representation | `broker.TestBrokerMetadataPermissionsAndNoSecrets` | PASS |
| Resolve requires both grant and secret policy | `broker.TestResolveAuthorizationAndResponse`, `broker.TestResolveDeniedWithoutSecretPolicy`, `broker.TestResolveDeniedWithoutGrant`, `broker.TestServiceAuthorizationWithoutSocket` | PASS |
| Rotate requires rotate grant and changes only target | `broker.TestRotateRequiresCapability`, `broker.TestRotateAuthorizedChangesOnlyTarget`, `broker.TestServiceRotationWithoutSocket` | PASS |
| Unix-socket protocol rejects invalid methods/fields/bodies | `broker.TestProtocolValidation` | PASS |
| Concurrent broker request handling and related broker suites are race-clean | `go test -race ./internal/broker` | PASS; Unix-bind E2E reports an explicit environment skip in the current sandbox; a recorder-based dual-policy unit test runs without a socket |
| Binding rechecks use stable IDs, not names | `broker.TestFindBindingByIDDoesNotConfuseGeneratedIDWithName`, `cmd.TestAccessGrantByBindingIDE2EResolveAndImmediateRevoke` | PASS |
| Interpreter-wide grants fail closed without explicit acknowledgement | `broker.TestInterpreterGrantRefusedByDefault` | PASS |
| CLI/TUI approval recovery cannot activate a cancelled grant | `broker.TestApprovalCommitCancellationRecomputesPolicyAndRemovesGrant`, `tui.TestApprovalCrashRecoveryProcess` | PASS |
| Approval recovery preserves unrelated/manual policy and concurrent administrative/grant requirements | `broker.TestRecoveryPreservesUnrelatedManualPolicy`, `TestRecoveryPreservesAdministrativePolicyCapturedForAffectedRecord`, `TestRecoveryPreservesConcurrentAdministrativeChange`, `TestRecoveryKeepsPolicyRequiredByConcurrentGrant`, `TestRecoverApprovalGrantPreservesUnrelatedManualPolicy` | PASS |

### 6.2 Dependency vulnerabilities

| Claim | Test | Result |
|-------|------|--------|
| No reachable known vulnerabilities in code or standard library | `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...` with Go 1.25.13+ | PASS on Go 1.25.13 after the 1.25.12 standard-library findings were remediated by the Go 1.25.13 toolchain floor |

### 6.3 TUI session and configuration lifecycle

| Claim | Test | Result |
|-------|------|--------|
| First unlock gets nonzero generation and stale approvals are rejected | `tui.TestFirstSessionGenerationFencesPreparedApproval` | PASS |
| Lock invalidates pending scratch results without nil dereference | `tui.TestStaleScratchDeleteNeverDereferencesLockedSession` | PASS |
| Ctrl+C and q both clear the vault session | `tui.TestCtrlCAndQZeroVaultSession` | PASS |
| Model fetch results cannot overwrite a newer request | `tui.TestStaleModelFetchRequestCannotOverwriteNewerSelection` | PASS |
| Missing profile key returns an error instead of panic | `tui.TestTUI_LaunchPrepared_*` plus launch tests | PASS |
| External editor uses private configured runtime and shell-free argv | `tui.TestExternalEditorUsesConfiguredPrivateRuntimeAndArguments` | PASS |
| Editor failure and interrupted startup leave no AegisKeys plaintext directory | `tui.TestExternalEditorFailureCallbackRemovesResidualDirectory`, `TestExternalEditorCleansInterruptedPrivateDirectory` | PASS |
| Stale editor preparation is deleted and never launches under a new session | `tui.TestStalePreparedEditorIsDeletedWithoutLaunch` | PASS |
| Superseded active-vault snapshots are zeroized through one replacement path | `tui.TestReplaceVaultSnapshotZeroesSupersededSecrets` | PASS |
| Stale wizard fetch cannot clear newer loading state | `tui.TestStaleWizardModelFetchDoesNotClearCurrentLoadingState` | PASS |
| Unchanged config overlay restores; child/user modification is preserved with conflict | `adapter.TestApplyFileWritesWithRestorePreservesChildModification`, existing restore tests | PASS |
| Request control sequences including DCS/C1 are removed | `tui.TestSanitizeLaunchOutputDropsDCSAndC1Controls` | PASS |

### 7. Saved profiles resolve before use

| Claim | Test | Result |
|-------|------|--------|
| Key/provider mismatch rejected | `cmd.TestValidateResolutionRejectsKeyProviderMismatch` | PASS |
| Unknown target app rejected at save-time validation | `cmd.TestValidateResolutionRejectsUnknownGenericTargetAtSaveTime` | PASS |

### 8. Config writing is honest about unsupported merge modes

| Claim | Test | Result |
|-------|------|--------|
| Existing user TOML is structurally merged | `adapter.TestApplyFileWrites_TOMLMergesExistingUserConfig` | PASS |
| Existing user XML is structurally patched | `adapter.TestApplyFileWrites_XMLPatchesExistingUserConfig` | PASS |
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
| Concurrent snapshot saves could resurrect deletes or overwrite existing records | Added flock and `MutateVault*` latest-state transactions |
| Raw secret argv flags exposed key material | Removed `key add --secret` and `vault add --secret`; prompt only |
| CLI profile create could save broken profiles | Added central `resolve.ValidateResolution` and render-mode derivation |
| TOML/XML “merge” could overwrite existing user config | Parser-backed structural TOML merge and identity-aware XML patch preserve unrelated entries |
| Provider metadata commands could display/export corrupted secret-bearing metadata | Redact provider CLI output; refuse export unless strict metadata validation passes |
| Audit logger trusted all future metadata callers | Pattern-redact audit event fields before write and force `0600` on the audit file |
| `adapter verify` default mode depended on locally installed target apps | Split render/files/no-leak verification from optional `--installed` smoke checks |

## Honest remaining gaps

| Gap | Risk | Mitigation needed |
|-----|------|-------------------|
| Sibling process isolation not explicit | Low | Out of scope (OS property) |
| Memory zeroization best-effort | Low | Out of scope (Go GC) |
| Adapter contracts marked `verified` may still need version-pinned real-application qualification | Medium | Maintain a separate real-launch matrix; fake executable smoke is contract evidence only |

## Central enforcement gate

```
adapter.ValidateLaunchStrategy(strategy, prof, prov, key, policy)
```

Called by: `ResolveLaunchStrategy`, `ResolveRunConfig`, profile save validation,
`runner.PrepareCommand`, and `runner.Run`.

## Honest scope boundary

**Protects against:** accidental leakage to terminal/logs/commits, plaintext persistence in config, broad shell env exposure, unsafe file permissions, manual apps receiving secrets, malformed vault resource abuse.

**Does NOT protect against:** child exfiltration (by design), same-user process inspection, concurrent profile edits clobbering, malware/root/kernel/keyloggers, shoulder surfing.
