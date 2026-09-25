package secret

import "errors"

// SessionMutation describes one small update to be applied to the latest
// encrypted vault state. Commands should use these transactional helpers
// instead of saving a previously loaded whole-vault snapshot.
type SessionMutation struct {
	// Preload is used to authenticate a target before mutation (for example,
	// to keep confirmation messages and launch validation fail-closed).
	Preload func(*Vault) error
	Mutate  func(*Vault) error
}

func (m SessionMutation) validate() error {
	if m.Mutate == nil {
		return errors.New("nil vault mutation")
	}
	return nil
}

// MutateVaultSession authenticates and then atomically applies one mutation.
// A non-empty password is authenticated and converted to its current derived
// key. An empty password requires the sessionKey previously obtained by
// LoadVaultWithKey (for example, from an OS-keyring-assisted session).
func MutateVaultSession(path, password string, sessionKey [32]byte, mutation SessionMutation) error {
	if err := mutation.validate(); err != nil {
		return err
	}
	if password != "" {
		latest, key, err := LoadVaultWithKey(path, password)
		if err != nil {
			return err
		}
		defer ZeroVault(latest)
		if mutation.Preload != nil {
			if err := mutation.Preload(latest); err != nil {
				return err
			}
		}
		return MutateVaultWithKey(path, key, mutation.Mutate)
	}
	if sessionKey == ([32]byte{}) {
		return errors.New("vault session key is required")
	}
	if mutation.Preload != nil {
		latest, err := LoadVaultByKey(path, sessionKey)
		if err != nil {
			return err
		}
		defer ZeroVault(latest)
		if err := mutation.Preload(latest); err != nil {
			return err
		}
	}
	return MutateVaultWithKey(path, sessionKey, mutation.Mutate)
}
