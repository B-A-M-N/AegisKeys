package broker

import (
	"errors"
	"time"

	"aegiskeys/internal/secret"
)

type bindingMutation func(*File) (map[string]bool, error)

// mutateBrokerPolicy is the common owner for lifecycle changes that alter
// binding or grant state. It only removes grant-derived requirements from the
// records explicitly affected by the mutation. Vault policy is reconciled
// before broker metadata is persisted: a crash or metadata write failure
// therefore fails closed because stale vault policy cannot authorize a
// disabled/rebound/deleted grant.
func mutateBrokerPolicy(path, vaultPath string, key [32]byte, mutate bindingMutation) error {
	return WithBrokerFileLocked(path, func(meta *File) error {
		if mutate == nil {
			return errors.New("nil broker lifecycle mutation")
		}
		affected, err := mutate(meta)
		if err != nil {
			return err
		}
		if len(affected) == 0 {
			return nil
		}
		if err := secret.MutateVaultWithKey(vaultPath, key, func(v *secret.Vault) error {
			needs := requiredBrokerPolicies(meta)
			for i := range v.Keys {
				if !affected[v.Keys[i].ID] {
					continue
				}
				reconcileCurrentPolicy(&v.Keys[i].Policy, needs[v.Keys[i].ID])
			}
			return nil
		}); err != nil {
			return err
		}
		return SaveBrokerFile(path, meta)
	})
}

func reconcileCurrentPolicy(policy *secret.SecretPolicy, need policyNeed) {
	if policy == nil {
		return
	}
	adminResolve := policy.BrokerAdminResolve || policy.AllowBrokerResolve && policy.BrokerResolveSource == secret.BrokerPolicySourceAdministrative
	adminRotate := policy.BrokerAdminRotate || policy.AllowBrokerRotate && policy.BrokerRotateSource == secret.BrokerPolicySourceAdministrative
	policy.BrokerAdminResolve = adminResolve
	policy.BrokerAdminRotate = adminRotate
	policy.AllowBrokerResolve = adminResolve || need.resolve
	policy.AllowBrokerRotate = adminRotate || need.rotate
	if policy.AllowBrokerResolve {
		if adminResolve {
			policy.BrokerResolveSource = secret.BrokerPolicySourceAdministrative
		} else {
			policy.BrokerResolveSource = secret.BrokerPolicySourceGrant
		}
	} else {
		policy.BrokerResolveSource = ""
	}
	if policy.AllowBrokerRotate {
		if adminRotate {
			policy.BrokerRotateSource = secret.BrokerPolicySourceAdministrative
		} else {
			policy.BrokerRotateSource = secret.BrokerPolicySourceGrant
		}
	} else {
		policy.BrokerRotateSource = ""
	}
	if policy.AllowBrokerResolve || policy.AllowBrokerRotate {
		policy.Version = 1
	}
}

// RevokeGrant disables a grant and reconciles grant-sourced policy for its
// target while preserving explicit administrative policy.
func RevokeGrant(path, vaultPath string, key [32]byte, grantID string) error {
	return mutateBrokerPolicy(path, vaultPath, key, func(meta *File) (map[string]bool, error) {
		g := findGrantByID(meta, grantID)
		if g == nil {
			return nil, errors.New("grant not found")
		}
		affected := map[string]bool{}
		if binding := meta.FindBindingByID(g.BindingID); binding != nil {
			affected[binding.SecretID] = true
		}
		g.Enabled = false
		return affected, nil
	})
}

// ReconcileExpiredGrants disables expired grants and removes stale
// grant-sourced vault policy. It is safe to call during broker startup.
func ReconcileExpiredGrants(path, vaultPath string, key [32]byte, now time.Time) error {
	return mutateBrokerPolicy(path, vaultPath, key, func(meta *File) (map[string]bool, error) {
		affected := map[string]bool{}
		for i := range meta.Grants {
			if !meta.Grants[i].Enabled || meta.Grants[i].ExpiresAt == nil || now.Before(*meta.Grants[i].ExpiresAt) {
				continue
			}
			if binding := meta.FindBindingByID(meta.Grants[i].BindingID); binding != nil {
				affected[binding.SecretID] = true
			}
			meta.Grants[i].Enabled = false
		}
		return affected, nil
	})
}

// RebindBinding points a binding at another vault record and suspends its
// grants. Policy for the old record is reconciled before metadata is saved.
func RebindBinding(path, vaultPath string, key [32]byte, bindingNameOrID, newSecretID string) error {
	if newSecretID == "" {
		return errors.New("new secret id is required")
	}
	return mutateBrokerPolicy(path, vaultPath, key, func(meta *File) (map[string]bool, error) {
		binding := findBindingByNameOrID(meta, bindingNameOrID)
		if binding == nil {
			return nil, errors.New("binding not found")
		}
		affected := map[string]bool{binding.SecretID: true, newSecretID: true}
		binding.SecretID = newSecretID
		binding.UpdatedAt = time.Now()
		for i := range meta.Grants {
			if meta.Grants[i].BindingID == binding.ID {
				meta.Grants[i].Enabled = false
			}
		}
		return affected, nil
	})
}

// DeleteBinding removes a binding, its grants, and pending intents, then
// reconciles grant-sourced policy for the former target.
func DeleteBinding(path, vaultPath string, key [32]byte, bindingNameOrID string) error {
	return mutateBrokerPolicy(path, vaultPath, key, func(meta *File) (map[string]bool, error) {
		binding := findBindingByNameOrID(meta, bindingNameOrID)
		if binding == nil {
			return nil, errors.New("binding not found")
		}
		bindingID := binding.ID
		affected := map[string]bool{binding.SecretID: true}
		keptBindings := meta.Bindings[:0]
		for _, candidate := range meta.Bindings {
			if candidate.ID != bindingID {
				keptBindings = append(keptBindings, candidate)
			}
		}
		meta.Bindings = keptBindings
		keptGrants := meta.Grants[:0]
		for _, grant := range meta.Grants {
			if grant.BindingID != bindingID {
				keptGrants = append(keptGrants, grant)
			}
		}
		meta.Grants = keptGrants
		keptIntents := meta.PendingApprovals[:0]
		for _, intent := range meta.PendingApprovals {
			if intent.BindingID != bindingID {
				keptIntents = append(keptIntents, intent)
			}
		}
		meta.PendingApprovals = keptIntents
		return affected, nil
	})
}

func findBindingByNameOrID(meta *File, value string) *CredentialBinding {
	if meta == nil {
		return nil
	}
	if binding := meta.FindBinding(value); binding != nil {
		return binding
	}
	return meta.FindBindingByID(value)
}
