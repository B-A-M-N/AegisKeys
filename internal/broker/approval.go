package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aegiskeys/internal/secret"
)

var ErrApprovalCancelled = errors.New("approval cancelled because the vault session changed")

// ApprovalGrantSpec is the non-secret grant staged before secret policy is
// enabled. Callers must create the IDs before invoking the stage operation.
type ApprovalGrantSpec struct {
	ID                 string
	Name               string
	BindingID          string
	Client             ClientConstraint
	Capabilities       []Capability
	ComponentAllowlist []string
	ExpiresAt          *time.Time
}

// StageApproval records a durable intent and a disabled grant without changing
// secret policy. It is shared by CLI and TUI approvals.
func StageApproval(ctx context.Context, path, vaultPath string, vaultKey [32]byte, bindingID string, grant ApprovalGrantSpec) (ApprovalIntent, error) {
	return stageApproval(ctx, path, vaultPath, vaultKey, nil, bindingID, grant)
}

// StageNewBindingApproval records a new binding, disabled grant, and durable
// intent in one metadata transaction. It is used by the quick CLI workflow.
func StageNewBindingApproval(ctx context.Context, path, vaultPath string, vaultKey [32]byte, binding CredentialBinding, grant ApprovalGrantSpec) (ApprovalIntent, error) {
	return stageApproval(ctx, path, vaultPath, vaultKey, &binding, binding.ID, grant)
}

func stageApproval(ctx context.Context, path, vaultPath string, vaultKey [32]byte, newBinding *CredentialBinding, bindingID string, grantSpec ApprovalGrantSpec) (ApprovalIntent, error) {
	if err := approvalContextErr(ctx); err != nil {
		return ApprovalIntent{}, err
	}
	meta, err := LoadBrokerFile(path)
	if err != nil {
		return ApprovalIntent{}, err
	}
	var binding *CredentialBinding
	if newBinding != nil {
		if meta.FindBinding(newBinding.Name) != nil || meta.FindBindingByID(newBinding.ID) != nil {
			return ApprovalIntent{}, errors.New("binding already exists")
		}
		binding = newBinding
	} else {
		binding = meta.FindBindingByID(bindingID)
	}
	if binding == nil {
		return ApprovalIntent{}, errors.New("binding not found")
	}
	if grantSpec.BindingID != binding.ID {
		return ApprovalIntent{}, errors.New("grant binding does not match approval binding")
	}
	v, err := secret.LoadVaultByKey(vaultPath, vaultKey)
	if err != nil {
		return ApprovalIntent{}, err
	}
	defer secret.ZeroVault(v)
	rec := v.Get(binding.SecretID)
	if rec == nil || rec.Archived {
		return ApprovalIntent{}, errors.New("binding target not found or archived")
	}
	now := time.Now()
	grant := AccessGrant{
		ID: grantSpec.ID, Name: grantSpec.Name, BindingID: binding.ID,
		Client: grantSpec.Client, Capabilities: append([]Capability(nil), grantSpec.Capabilities...),
		ComponentAllowlist: grantComponentAllowlist(grantSpec.ComponentAllowlist),
		Enabled:            false, CreatedAt: now, ExpiresAt: grantSpec.ExpiresAt,
	}
	intent := ApprovalIntent{
		GrantID: grant.ID, BindingID: binding.ID, SecretID: binding.SecretID,
		Capabilities:       append([]Capability(nil), grantSpec.Capabilities...),
		PriorAllowResolve:  rec.Policy.AllowBrokerResolve,
		PriorAllowRotate:   rec.Policy.AllowBrokerRotate,
		PriorResolveSource: rec.Policy.BrokerResolveSource,
		PriorRotateSource:  rec.Policy.BrokerRotateSource,
		PriorPolicyKnown:   true,
		AdminRevision:      rec.Policy.BrokerAdminRevision,
		CreatedAt:          now,
	}
	if err := approvalContextErr(ctx); err != nil {
		return ApprovalIntent{}, err
	}
	if err := TransactBrokerFile(path, func(latest *File) error {
		if err := approvalContextErr(ctx); err != nil {
			return err
		}
		if newBinding != nil {
			if latest.FindBinding(newBinding.Name) != nil || latest.FindBindingByID(newBinding.ID) != nil {
				return errors.New("binding already exists")
			}
			latest.Bindings = append(latest.Bindings, *newBinding)
		} else {
			current := latest.FindBindingByID(bindingID)
			if current == nil || current.SecretID != binding.SecretID {
				return errors.New("binding changed during approval setup")
			}
		}
		for _, existing := range latest.Grants {
			if existing.ID == grant.ID {
				return errors.New("grant already exists")
			}
		}
		latest.Grants = append(latest.Grants, grant)
		latest.PendingApprovals = append(latest.PendingApprovals, intent)
		return nil
	}); err != nil {
		return ApprovalIntent{}, err
	}
	return intent, nil
}

// CommitApproval enables policy and the staged grant while the broker metadata
// lock is held. If activation fails, the staged grant is removed and policy is
// recomputed from every currently enabled grant; cleanup failures are surfaced.
func CommitApproval(ctx context.Context, path, vaultPath string, vaultKey [32]byte, grantID string) error {
	return CommitApprovalObserved(ctx, path, vaultPath, vaultKey, grantID, nil)
}

// CommitApprovalObserved is CommitApproval with a test-only durable-boundary
// observer. Production callers use CommitApproval; subprocess recovery tests
// can terminate after the vault policy write but before grant activation.
func CommitApprovalObserved(ctx context.Context, path, vaultPath string, vaultKey [32]byte, grantID string, observe func(string)) error {
	if err := approvalContextErr(ctx); err != nil {
		return errors.Join(err, CancelStagedApproval(path, grantID))
	}
	err := TransactBrokerFile(path, func(meta *File) error {
		if err := approvalContextErr(ctx); err != nil {
			return err
		}
		grant := findGrantByID(meta, grantID)
		if grant == nil {
			return errors.New("staged grant missing")
		}
		intent := findIntentByGrantID(meta, grantID)
		if intent == nil {
			return errors.New("approval intent missing")
		}
		binding := meta.FindBindingByID(grant.BindingID)
		if binding == nil || binding.SecretID != intent.SecretID {
			return errors.New("binding changed after approval was staged")
		}
		if err := secret.MutateVaultWithKey(vaultPath, vaultKey, func(v *secret.Vault) error {
			rec := v.Get(binding.SecretID)
			if rec == nil || rec.Archived {
				return errors.New("binding target not found or archived")
			}
			if err := applyGrantPolicy(rec, grant.Capabilities); err != nil {
				return err
			}
			rec.Policy.Version = 1
			return nil
		}); err != nil {
			return err
		}
		if observe != nil {
			observe("after-policy")
		}
		// Cancellation after the vault write but before metadata activation must
		// not leave a live grant behind. The caller then performs authoritative
		// policy recomputation during recovery.
		if err := approvalContextErr(ctx); err != nil {
			return err
		}
		grant.Enabled = true
		removeIntent(meta, grantID)
		return nil
	})
	if err == nil {
		return nil
	}
	cleanupErr := recoverApprovalGrant(path, vaultPath, vaultKey, grantID)
	return errors.Join(err, cleanupErr)
}

// CancelStagedApproval removes a disabled staged grant and intent. It does not
// touch policy because staging never enables policy.
func CancelStagedApproval(path, grantID string) error {
	if grantID == "" {
		return nil
	}
	return MutateBrokerFile(path, func(meta *File) error {
		removeGrant(meta, grantID)
		removeIntent(meta, grantID)
		return nil
	})
}

// RecoverPendingApprovals finalizes enabled intents and reconciles only vault
// records named by pending intents. Administrative/manual policy is preserved;
// grant-sourced policy is recomputed from currently enabled grants.
func RecoverPendingApprovals(path, vaultPath string, vaultKey [32]byte) error {
	return WithBrokerFileLocked(path, func(meta *File) error {
		if len(meta.PendingApprovals) == 0 {
			return nil
		}
		processed := make(map[string]bool, len(meta.PendingApprovals))
		for _, intent := range meta.PendingApprovals {
			processed[intent.GrantID] = true
			if findGrantByID(meta, intent.GrantID) == nil || !findGrantByID(meta, intent.GrantID).Enabled {
				removeGrant(meta, intent.GrantID)
			}
		}
		required := requiredBrokerPolicies(meta)
		baselines := approvalBaselines(meta)
		affected := make(map[string]bool, len(baselines))
		for secretID := range baselines {
			affected[secretID] = true
		}
		if err := secret.MutateVaultWithKey(vaultPath, vaultKey, func(v *secret.Vault) error {
			for i := range v.Keys {
				rec := &v.Keys[i]
				if !affected[rec.ID] {
					continue
				}
				need := required[rec.ID]
				baseline, ok := baselines[rec.ID]
				if !ok {
					continue
				}
				reconcilePolicyForSecret(&rec.Policy, baseline, need)
			}
			return nil
		}); err != nil {
			return err
		}
		intents := meta.PendingApprovals[:0]
		for _, intent := range meta.PendingApprovals {
			if !processed[intent.GrantID] {
				intents = append(intents, intent)
			}
		}
		meta.PendingApprovals = intents
		return SaveBrokerFile(path, meta)
	})
}

type policyNeed struct{ resolve, rotate bool }

type policyBaseline struct {
	resolve, rotate             bool
	resolveSource, rotateSource secret.BrokerPolicySource
	adminRevision               uint64
}

func approvalBaselines(meta *File) map[string]policyBaseline {
	out := make(map[string]policyBaseline, len(meta.PendingApprovals))
	for _, intent := range meta.PendingApprovals {
		baseline := policyBaseline{}
		if intent.PriorPolicyKnown {
			baseline.resolve = intent.PriorAllowResolve || intent.PriorResolveSource == secret.BrokerPolicySourceAdministrative
			baseline.rotate = intent.PriorAllowRotate || intent.PriorRotateSource == secret.BrokerPolicySourceAdministrative
			baseline.resolveSource = intent.PriorResolveSource
			baseline.rotateSource = intent.PriorRotateSource
			baseline.adminRevision = intent.AdminRevision
		} else {
			// Legacy intent has only booleans. Existing true is the authoritative
			// administrative baseline; false remains authoritative deny.
			baseline.resolve = intent.PriorAllowResolve
			baseline.rotate = intent.PriorAllowRotate
			if baseline.resolve {
				baseline.resolveSource = secret.BrokerPolicySourceAdministrative
			}
			if baseline.rotate {
				baseline.rotateSource = secret.BrokerPolicySourceAdministrative
			}
		}
		if prior, ok := out[intent.SecretID]; ok {
			baseline.resolve = baseline.resolve || prior.resolve
			baseline.rotate = baseline.rotate || prior.rotate
			if prior.resolve {
				baseline.resolveSource = secret.BrokerPolicySourceAdministrative
			}
			if prior.rotate {
				baseline.rotateSource = secret.BrokerPolicySourceAdministrative
			}
		}
		out[intent.SecretID] = baseline
	}
	return out
}

func effectivePolicySource(enabled bool, required bool) secret.BrokerPolicySource {
	if enabled {
		return secret.BrokerPolicySourceAdministrative
	}
	if required {
		return secret.BrokerPolicySourceGrant
	}
	return ""
}

func reconcilePolicyForSecret(policy *secret.SecretPolicy, baseline policyBaseline, required policyNeed) {
	// Explicit administrative state is authoritative and is not reconstructed
	// from an older approval baseline. A later admin deny therefore wins.
	if policy.BrokerAdminRevision != baseline.adminRevision {
		baseline.resolve = policy.BrokerAdminResolve
		baseline.rotate = policy.BrokerAdminRotate
	}
	currentResolveAdmin := policy.AllowBrokerResolve && policy.BrokerResolveSource == secret.BrokerPolicySourceAdministrative
	currentRotateAdmin := policy.AllowBrokerRotate && policy.BrokerRotateSource == secret.BrokerPolicySourceAdministrative
	policy.AllowBrokerResolve = baseline.resolve || currentResolveAdmin || required.resolve
	policy.AllowBrokerRotate = baseline.rotate || currentRotateAdmin || required.rotate
	resolveSource := secret.BrokerPolicySource("")
	if baseline.resolve {
		resolveSource = secret.BrokerPolicySourceAdministrative
	}
	if currentResolveAdmin {
		resolveSource = secret.BrokerPolicySourceAdministrative
	}
	if required.resolve && resolveSource == "" {
		resolveSource = secret.BrokerPolicySourceGrant
	}
	rotateSource := secret.BrokerPolicySource("")
	if baseline.rotate {
		rotateSource = secret.BrokerPolicySourceAdministrative
	}
	if currentRotateAdmin {
		rotateSource = secret.BrokerPolicySourceAdministrative
	}
	if required.rotate && rotateSource == "" {
		rotateSource = secret.BrokerPolicySourceGrant
	}
	policy.BrokerResolveSource = resolveSource
	policy.BrokerRotateSource = rotateSource
	if policy.AllowBrokerResolve || policy.AllowBrokerRotate {
		policy.Version = 1
	}
}

func applyGrantPolicy(rec *secret.SecretRecord, capabilities []Capability) error {
	if rec == nil {
		return errors.New("binding target missing")
	}
	for _, capability := range capabilities {
		switch capability {
		case CapabilityResolve:
			if rec.Policy.BrokerResolveSource != secret.BrokerPolicySourceAdministrative {
				rec.Policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
			}
			rec.Policy.AllowBrokerResolve = true
		case CapabilityRotate:
			if rec.Policy.BrokerRotateSource != secret.BrokerPolicySourceAdministrative {
				rec.Policy.BrokerRotateSource = secret.BrokerPolicySourceGrant
			}
			rec.Policy.AllowBrokerRotate = true
		default:
			return fmt.Errorf("invalid staged capability %q", capability)
		}
	}
	rec.Policy.Version = 1
	return nil
}

func requiredBrokerPolicies(meta *File) map[string]policyNeed {
	secretByBinding := make(map[string]string, len(meta.Bindings))
	for _, binding := range meta.Bindings {
		secretByBinding[binding.ID] = binding.SecretID
	}
	out := make(map[string]policyNeed)
	now := time.Now()
	for _, grant := range meta.Grants {
		if !grant.Enabled || grant.ExpiresAt != nil && !now.Before(*grant.ExpiresAt) {
			continue
		}
		secretID := secretByBinding[grant.BindingID]
		if secretID == "" {
			continue
		}
		need := out[secretID]
		for _, capability := range grant.Capabilities {
			switch capability {
			case CapabilityResolve:
				need.resolve = true
			case CapabilityRotate:
				need.rotate = true
			}
		}
		out[secretID] = need
	}
	return out
}

func recoverApprovalGrant(path, vaultPath string, vaultKey [32]byte, grantID string) error {
	return WithBrokerFileLocked(path, func(meta *File) error {
		secretID := ""
		if grant := findGrantByID(meta, grantID); grant != nil {
			if binding := meta.FindBindingByID(grant.BindingID); binding != nil {
				secretID = binding.SecretID
			}
		}
		if intent := findIntentByGrantID(meta, grantID); intent != nil {
			secretID = intent.SecretID
		}
		baseline, ok := approvalBaselineForGrant(meta, grantID)
		if !ok {
			return errors.New("approval baseline missing")
		}
		removeGrant(meta, grantID)
		removeIntent(meta, grantID)
		required := requiredBrokerPolicies(meta)
		if err := secret.MutateVaultWithKey(vaultPath, vaultKey, func(v *secret.Vault) error {
			rec := v.Get(secretID)
			if rec == nil {
				return nil
			}
			need := required[rec.ID]
			reconcilePolicyForSecret(&rec.Policy, baseline, need)
			return nil
		}); err != nil {
			return err
		}
		return SaveBrokerFile(path, meta)
	})
}

func approvalBaselineForGrant(meta *File, grantID string) (policyBaseline, bool) {
	intent := findIntentByGrantID(meta, grantID)
	if intent == nil {
		return policyBaseline{}, false
	}
	baseline := policyBaseline{
		resolve:       intent.PriorAllowResolve || intent.PriorResolveSource == secret.BrokerPolicySourceAdministrative,
		rotate:        intent.PriorAllowRotate || intent.PriorRotateSource == secret.BrokerPolicySourceAdministrative,
		resolveSource: intent.PriorResolveSource, rotateSource: intent.PriorRotateSource,
		adminRevision: intent.AdminRevision,
	}
	if !intent.PriorPolicyKnown {
		baseline.resolve = intent.PriorAllowResolve
		baseline.rotate = intent.PriorAllowRotate
		baseline.resolveSource, baseline.rotateSource = "", ""
		if baseline.resolve {
			baseline.resolveSource = secret.BrokerPolicySourceAdministrative
		}
		if baseline.rotate {
			baseline.rotateSource = secret.BrokerPolicySourceAdministrative
		}
	}
	return baseline, true
}

func removeGrant(meta *File, grantID string) {
	grants := meta.Grants[:0]
	for _, grant := range meta.Grants {
		if grant.ID != grantID {
			grants = append(grants, grant)
		}
	}
	meta.Grants = grants
}

func removeIntent(meta *File, grantID string) {
	intents := meta.PendingApprovals[:0]
	for _, intent := range meta.PendingApprovals {
		if intent.GrantID != grantID {
			intents = append(intents, intent)
		}
	}
	meta.PendingApprovals = intents
}

func findGrantByID(meta *File, grantID string) *AccessGrant {
	for i := range meta.Grants {
		if meta.Grants[i].ID == grantID {
			return &meta.Grants[i]
		}
	}
	return nil
}

func findIntentByGrantID(meta *File, grantID string) *ApprovalIntent {
	for i := range meta.PendingApprovals {
		if meta.PendingApprovals[i].GrantID == grantID {
			return &meta.PendingApprovals[i]
		}
	}
	return nil
}

func approvalContextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("approval context is required")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrApprovalCancelled, ctx.Err())
	default:
		return nil
	}
}
