package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"

	"aegiskeys/internal/broker"
	"aegiskeys/internal/config"
	"aegiskeys/internal/secret"
)

func TestAccessApprovalStagesThenActivatesAfterConfirmedPolicyChange(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "key_1", Label: "Main", Secret: "secret", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "app/main", SecretID: "key_1"})
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "app")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	msg := prepareAccessApprovalCmd(dir, "binding_1", key, exe, "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))()
	prepared, ok := msg.(accessApprovalPreparedMsg)
	if !ok {
		t.Fatalf("prepare returned %T", msg)
	}
	staged, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(staged.Grants) != 1 || staged.Grants[0].Enabled {
		t.Fatal("grant was not staged disabled")
	}
	done := commitStagedAccessCmd(dir, prepared.pending)().(accessMutationDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	final, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(final.Grants) != 1 || !final.Grants[0].Enabled {
		t.Fatal("confirmed grant not activated")
	}
}

func TestPrepareRejectsMissingKeyBeforeStaging(t *testing.T) {
	dir := t.TempDir()
	if err := secret.InitVault(config.VaultPath(dir), "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(config.VaultPath(dir), "pw")
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "app/missing", SecretID: "key_missing"})
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "app")
	_ = os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700)
	msg := prepareAccessApprovalCmd(dir, "binding_1", key, exe, "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))()
	if _, ok := msg.(accessApprovalPreparedMsg); ok {
		t.Fatal("missing key was staged")
	}
	got, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(got.Grants) != 0 || len(got.PendingApprovals) != 0 {
		t.Fatal("missing-key preparation left authorization state")
	}
}

func TestRecoverPendingApprovalRollsBackIncompleteAndFinalizesCommitted(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "k1", Label: "One", Secret: "s1", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "k2", Label: "Two", Secret: "s2", Policy: secret.SecretPolicy{Version: 1, AllowBrokerResolve: true}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "b1", Name: "app/one", SecretID: "k1"}, broker.CredentialBinding{ID: "b2", Name: "app/two", SecretID: "k2"})
	g := func(id, bid string, enabled bool) broker.AccessGrant {
		return broker.AccessGrant{ID: id, Name: id, BindingID: bid, Client: broker.ClientConstraint{UID: 1, ExecutablePath: "/x"}, Capabilities: []broker.Capability{broker.CapabilityResolve}, Enabled: enabled, CreatedAt: time.Now()}
	}
	meta.Grants = []broker.AccessGrant{g("incomplete", "b1", false), g("committed", "b2", true)}
	resolve := []broker.Capability{broker.CapabilityResolve}
	meta.PendingApprovals = []broker.ApprovalIntent{{GrantID: "incomplete", BindingID: "b1", SecretID: "k1", Capabilities: resolve, PriorPolicyKnown: true, CreatedAt: time.Now()}, {GrantID: "committed", BindingID: "b2", SecretID: "k2", Capabilities: resolve, PriorAllowResolve: true, PriorPolicyKnown: true, CreatedAt: time.Now()}}
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		rec := v.Get("k1")
		rec.Policy.AllowBrokerResolve = true
		rec.Policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	msg := recoverPendingApprovalsCmd(dir, key)().(accessMutationDoneMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	if v.Get("k1").Policy.AllowBrokerResolve {
		t.Fatal("incomplete approval policy not rolled back")
	}
	if !v.Get("k2").Policy.AllowBrokerResolve {
		t.Fatal("committed approval policy changed")
	}
	got, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(got.Grants) != 1 || got.Grants[0].ID != "committed" || len(got.PendingApprovals) != 0 {
		t.Fatalf("bad recovery state: %+v", got)
	}
}

func TestRecoveryPreservesPolicyRequiredBySurvivingSameSecretGrant(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		return v.Add(secret.SecretRecord{ID: "k", Label: "Key", Secret: "s", Policy: secret.SecretPolicy{Version: 1, AllowBrokerResolve: true, AllowBrokerRotate: true, BrokerResolveSource: secret.BrokerPolicySourceGrant, BrokerRotateSource: secret.BrokerPolicySourceGrant}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "b", Name: "app/key", SecretID: "k"})
	meta.Grants = []broker.AccessGrant{
		{ID: "survivor", Name: "survivor", BindingID: "b", Client: broker.ClientConstraint{UID: 1, ExecutablePath: "/survivor"}, Capabilities: []broker.Capability{broker.CapabilityResolve}, Enabled: true, CreatedAt: time.Now()},
		{ID: "stale", Name: "stale", BindingID: "b", Client: broker.ClientConstraint{UID: 1, ExecutablePath: "/stale"}, Capabilities: []broker.Capability{broker.CapabilityRotate}, CreatedAt: time.Now()},
	}
	meta.PendingApprovals = []broker.ApprovalIntent{{GrantID: "stale", BindingID: "b", SecretID: "k", Capabilities: []broker.Capability{broker.CapabilityRotate}, PriorAllowResolve: true, PriorAllowRotate: false, PriorResolveSource: secret.BrokerPolicySourceAdministrative, PriorPolicyKnown: true, CreatedAt: time.Now()}}
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), meta); err != nil {
		t.Fatal(err)
	}
	msg := recoverPendingApprovalsCmd(dir, key)().(accessMutationDoneMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	rec := v.Get("k")
	if !rec.Policy.AllowBrokerResolve {
		t.Fatal("recovery removed policy required by surviving grant")
	}
	if rec.Policy.AllowBrokerRotate {
		t.Fatal("recovery kept policy for removed incomplete grant")
	}
	got, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(got.Grants) != 1 || got.Grants[0].ID != "survivor" || len(got.PendingApprovals) != 0 {
		t.Fatalf("bad recovered state: %+v", got)
	}
}

func TestStaleStagedGrantCleanupPreservesEnabledAndRecentGrants(t *testing.T) {
	dir := t.TempDir()
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "b", Name: "app/x", SecretID: "k"})
	old := broker.AccessGrant{ID: "old", Name: "x", BindingID: "b", Client: broker.ClientConstraint{UID: 1, ExecutablePath: "/x"}, Capabilities: []broker.Capability{broker.CapabilityResolve}, CreatedAt: time.Now().Add(-time.Hour)}
	recent := old
	recent.ID = "recent"
	recent.CreatedAt = time.Now()
	enabled := old
	enabled.ID = "enabled"
	enabled.Enabled = true
	enabled.CreatedAt = time.Now().Add(-time.Hour)
	meta.Grants = []broker.AccessGrant{old, recent, enabled}
	_ = broker.SaveBrokerFile(config.BrokerPath(dir), meta)
	msg := cleanupStaleStagedAccessCmd(dir, 30*time.Minute)().(accessMutationDoneMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	got, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(got.Grants) != 2 || got.Grants[0].ID != "recent" || got.Grants[1].ID != "enabled" {
		t.Fatalf("unexpected cleanup result: %+v", got.Grants)
	}
}

func TestAccessApprovalRebindAfterStagingFailsAndRollsBackPolicy(t *testing.T) {
	dir := t.TempDir()
	vaultPath := config.VaultPath(dir)
	if err := secret.InitVault(vaultPath, "pw"); err != nil {
		t.Fatal(err)
	}
	_, key, _ := secret.LoadVaultWithKey(vaultPath, "pw")
	if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
		if err := v.Add(secret.SecretRecord{ID: "key_1", Label: "One", Secret: "s1", Policy: secret.SecretPolicy{Version: 1}}); err != nil {
			return err
		}
		return v.Add(secret.SecretRecord{ID: "key_2", Label: "Two", Secret: "s2", Policy: secret.SecretPolicy{Version: 1}})
	}); err != nil {
		t.Fatal(err)
	}
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "app/main", SecretID: "key_1"})
	_ = broker.SaveBrokerFile(config.BrokerPath(dir), meta)
	exe := filepath.Join(dir, "app")
	_ = os.WriteFile(exe, []byte("#!/bin/sh\n"), 0700)
	prepared := prepareAccessApprovalCmd(dir, "binding_1", key, exe, "resolve", time.Now().Add(time.Hour).Format(time.RFC3339))().(accessApprovalPreparedMsg)
	_ = broker.MutateBrokerFile(config.BrokerPath(dir), func(m *broker.File) error { m.Bindings[0].SecretID = "key_2"; return nil })
	done := commitStagedAccessCmd(dir, prepared.pending)().(accessMutationDoneMsg)
	if done.err == nil {
		t.Fatal("rebound binding accepted")
	}
	v, _ := secret.LoadVaultByKey(vaultPath, key)
	rec := v.Get("key_1")
	if rec.Policy.AllowBrokerResolve {
		t.Fatal("failed activation left policy enabled")
	}
	final, _ := broker.LoadBrokerFile(config.BrokerPath(dir))
	if len(final.Grants) != 0 {
		t.Fatal("failed activation left staged grant")
	}
}

func TestAccessScreenShowsMetadataOnly(t *testing.T) {
	m := newTestModel(t)
	meta := broker.NewFile()
	meta.Bindings = append(meta.Bindings, broker.CredentialBinding{ID: "binding_1", Name: "athena/openrouter", SecretID: "key_1"})
	meta.Grants = append(meta.Grants, broker.AccessGrant{
		ID: "grant_1", Name: "Athena", BindingID: "binding_1",
		Client:       broker.ClientConstraint{UID: 1000, ExecutablePath: "/opt/athena/bin/athena"},
		Capabilities: []broker.Capability{broker.CapabilityResolve}, Enabled: true, CreatedAt: time.Now(),
	})
	if err := broker.SaveBrokerFile(config.BrokerPath(m.configDir), meta); err != nil {
		t.Fatal(err)
	}
	m.brokerMeta = meta
	m.unlocked = true
	m.vaultSession = &vaultSession{vault: &secret.Vault{Keys: []secret.SecretRecord{{ID: "key_1", Label: "Primary", Secret: "raw-never-shown-123456"}}}}
	m.active = screenAccess
	content := stripANSIForTest(m.View().Content)
	if !strings.Contains(content, "athena/openrouter") || !strings.Contains(content, "athena") || !strings.Contains(content, "resolve") {
		t.Fatalf("access metadata missing: %q", content)
	}
	if strings.Contains(content, "raw-never-shown") {
		t.Fatalf("access screen exposed sensitive or internal identifier: %q", content)
	}
}

func TestFirstSessionGenerationFencesPreparedApproval(t *testing.T) {
	m := newTestModel(t)
	m.scratchBodyInput = textarea.New()
	m.scratchTitleInput = textinput.New()
	m.unlocked = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	gen := m.sessionGen
	if gen == 0 {
		t.Fatal("first unlock retained zero generation")
	}
	_, cmd := m.Update(accessApprovalPreparedMsg{sessionGen: gen - 1, pending: accessApprovalPending{GrantID: "stale"}})
	if cmd == nil {
		t.Fatal("stale prepared approval was not cleaned")
	}
	if m.modal == modalAccessConfirm {
		t.Fatal("stale approval opened confirmation modal")
	}
}

func TestStaleScratchDeleteNeverDereferencesLockedSession(t *testing.T) {
	m := newTestModel(t)
	m.scratchBodyInput = textarea.New()
	m.scratchBodyInput.SetWidth(40)
	m.scratchBodyInput.SetHeight(8)
	m.scratchTitleInput = textinput.New()
	m.unlocked = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	m.lockVault()
	_, _ = m.Update(scratchDeletedMsg{id: "x", sessionGen: m.sessionGen - 1})
	if m.unlocked || m.vaultSession != nil {
		t.Fatal("locked session changed")
	}
}

func TestAccessEnterInspectsSelectedGrant(t *testing.T) {
	m := newTestModel(t)
	m.brokerMeta = broker.NewFile()
	m.brokerMeta.Bindings = append(m.brokerMeta.Bindings, broker.CredentialBinding{ID: "b", Name: "app/key", SecretID: "k"})
	m.brokerMeta.Grants = append(m.brokerMeta.Grants, broker.AccessGrant{ID: "g", Name: "App", BindingID: "b", Client: broker.ClientConstraint{UID: 1, ExecutablePath: "/x/app/very-long-path"}, Capabilities: []broker.Capability{broker.CapabilityResolve}, Enabled: true})
	m.active = screenAccess
	m.focus = focusContent
	m.selected[screenAccess] = 1
	sendKey(t, m, "enter")
	if m.modal != modalDetail {
		t.Fatal("selected grant did not open detail")
	}
	out := stripANSIForTest(m.View().Content)
	for _, want := range []string{"/x/app/very-long-path", "ENABLED"} {
		if !strings.Contains(out, want) {
			t.Fatalf("grant detail missing %q: %s", want, out)
		}
	}
}

func TestBindingMutationCommandsAreSessionFenced(t *testing.T) {
	dir := t.TempDir()
	if err := secret.InitVault(config.VaultPath(dir), "pw"); err != nil {
		t.Fatal(err)
	}
	if err := broker.SaveBrokerFile(config.BrokerPath(dir), broker.NewFile()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	created := createAccessBindingSessionCmd(ctx, dir, "app/key", "key_1", 7)().(accessMutationDoneMsg)
	if !errors.Is(created.err, context.Canceled) || created.sessionGen != 7 {
		t.Fatalf("create was not session-fenced: %+v", created)
	}
	rebound := rebindAccessBindingSessionCmd(ctx, dir, "binding_1", "key_2", 7, [32]byte{})().(accessMutationDoneMsg)
	if !errors.Is(rebound.err, context.Canceled) || rebound.sessionGen != 7 {
		t.Fatalf("rebind was not session-fenced: %+v", rebound)
	}
	meta, err := broker.LoadBrokerFile(config.BrokerPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Bindings) != 0 {
		t.Fatalf("cancelled binding mutation changed metadata: %+v", meta.Bindings)
	}
}

func TestApprovalCommitCompletionCarriesSessionGeneration(t *testing.T) {
	m := newTestModel(t)
	m.unlocked = true
	m.vaultSession = &vaultSession{}
	m.unlockSessionContext()
	msg := accessMutationDoneMsg{message: "Access grant approved.", sessionGen: m.sessionGen}
	_, _ = m.Update(msg)
	if m.statusMsg != msg.message {
		t.Fatalf("current completion was not accepted: %q", m.statusMsg)
	}
	stale := accessMutationDoneMsg{message: "stale", sessionGen: m.sessionGen - 1}
	_, _ = m.Update(stale)
	if m.statusMsg != msg.message {
		t.Fatalf("stale completion changed current session: %q", m.statusMsg)
	}
}
