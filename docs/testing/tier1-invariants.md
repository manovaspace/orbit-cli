# Tier-1 invariants

Tier 1 covers the five security and integrity properties of `orbit` and `orbit-server` that must hold on every change. Each invariant names the code that enforces it and the test that asserts it. Tests run with `go test ./...` and are hermetic (`t.TempDir()`, fake runners, mock servers).

| Invariant | Where enforced | Test |
| --- | --- | --- |
| 1. Secret files are written with mode `0600` | `pkg/owner/infrastructure/filestore/store.go` (`owner.json`), `pkg/invite/infrastructure/filestore/store.go`, `pkg/config/infrastructure/osfile/osfile.go` (`config.yaml`, auto-tightened on load), `pkg/env/application/application.go` (generated `.env`, `.cursor/mcp.env`), `pkg/onboard/infrastructure/sessionstore/manager.go`, `pkg/onboard/infrastructure/hostfs/host.go`, `pkg/onboard/infrastructure/sshkeys/keys.go`, `pkg/tui/onboard/stage_identity.go` | `pkg/owner/infrastructure/filestore:TestStore_SaveAndLoad`, `pkg/owner/infrastructure/filestore:TestStore_InsecurePermissions`, `pkg/config/infrastructure/osfile:TestLoadAutoTightensPermissions`, `pkg/config/infrastructure/osfile:TestAtomicWritePermissions`, `pkg/invite/infrastructure/filestore:TestInviteStoreLifecycle`, `pkg/env/application:TestSetupGeneratesAndSkips`, `pkg/onboard/infrastructure/sessionstore:TestSessionCheckpointAndResume`, `pkg/onboard/infrastructure/hostfs:TestWriteWireGuard`, `pkg/onboard/infrastructure/sshkeys:TestEnsureKeyGeneratesWithGoCryptoWhenSSHKeygenFails`, `pkg/tui/onboard:TestIdentitySSHKeyGeneration` |
| 2. Wrong or missing HMAC returns `401` | Staff request signing and verification: `pkg/staffhmac/hmac.go` (`Verify`), enforced in `test/testbed/mockserver/main.go` (`wrapStaffHMAC`). Invite-token signature check: `pkg/invite/domain/token.go` (`hmac.Equal`), mapped to `401` in `pkg/onboard/infrastructure/server/server.go` (`handleClaim`) | `test/testbed/mockserver:TestStaffHMACAuthenticationAndReservedUserGuards`, `pkg/onboard/infrastructure/server:TestClaim_InvalidOrExpiredToken`, `pkg/invite/domain:TestValidateToken_TamperedSignature` |
| 3. Third failed grant attempt burns the grant | `pkg/owner/domain/grant.go` (`GrantManager.VerifyGrant`: attempt counter, purge or mark-used on the third strike, both in-memory and store-backed) | `pkg/owner/domain:TestGrantBurnInvariantThirdStrikeRejectsCorrectCode`, `pkg/owner/domain:TestGrantManagerMaxAttempts`, `test/testbed/mockserver:TestGrantThreeStrikeBurn` |
| 4. Git operations are non-destructive | `pkg/workspace/application/service.go` (`syncRepo` skips uncommitted repos and fast-forwards only; `repairTarget` copies `.git` only), `pkg/workspace/infrastructure/gitcli/gitcli.go` (argv for every git call) | `pkg/workspace/infrastructure/gitcli:TestNonDestructiveInvariantGitArgv`, `pkg/workspace/application:TestSyncReasons`, `pkg/workspace/application:TestRepairCopiesGitWithoutRemovingWorkingTree`, `pkg/workspace:TestRepairIsNonDestructive` |
| 5. Secrets are redacted in all output | `pkg/config/domain/domain.go` (`IsSecretKey`, `Masked`, `MaskCustom`, `Entries`), `pkg/config/application/service.go` (`Show`, `List`, `Get`; `GetRaw` only for `config get --raw`), `cmd/orbit/config.go` (`config get`) | `pkg/config/application:TestRedactionInvariantSecretKeysMasked`, `pkg/config/application:TestGetMasksSecretUnlessRaw`, `pkg/config/application:TestGetRawReturnsPlaintextSecret`, `pkg/config/application:TestListMasksNestedSecretEntries`, `pkg/config/domain:TestMaskedDoesNotMutateOriginal`, `pkg/config/domain:TestMaskedMasksNestedSecrets`, `pkg/config/domain:TestMaskedNilCustomStaysNil`, `cmd/orbit:TestConfigGetMasksSecretUnlessRaw`, `cmd/orbit:TestConfigOutputPathsNeverLeakSecretCanary` |

## Rule

A change that breaks one of these invariants fails its named test. Do not delete or weaken these tests. If a test is wrong, fix the test with the owner of the invariant in review, not by relaxing the assertion.

Run the Tier-1 set with:

```bash
go test ./pkg/owner/... ./pkg/config/... ./pkg/invite/... ./pkg/env/... ./pkg/onboard/... ./pkg/tui/onboard/... ./pkg/workspace/... ./test/testbed/mockserver/...
```
