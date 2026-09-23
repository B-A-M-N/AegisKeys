package secret

// ZeroRecord clears decrypted secret material from a record.
//
// Go string values are immutable and runtime-managed, so this is best-effort
// memory hygiene: it removes references and makes values unavailable through
// this object, but cannot guarantee that every byte has been physically
// overwritten. Derived [32]byte keys must still be explicitly zeroed by their
// owner.
func ZeroRecord(r *SecretRecord) {
	if r == nil {
		return
	}
	r.Secret = ""
	r.PrivateNote = ""
	for i := range r.ExtraSecrets {
		r.ExtraSecrets[i].Secret = ""
	}
	r.ExtraSecrets = nil
}

// ZeroRecordScratchPad clears the decrypted body of a scratchpad. See
// ZeroRecord for the best-effort memory-hygiene guarantee provided for Go
// string data.
func ZeroScratchPad(r *ScratchPadRecord) {
	if r == nil {
		return
	}
	r.Body = ""
}

// ZeroVault clears all decrypted secret material from a vault, including
// secondary credential components and scratchpad bodies.
func ZeroVault(v *Vault) {
	if v == nil {
		return
	}
	for i := range v.Keys {
		ZeroRecord(&v.Keys[i])
	}
	for i := range v.ScratchPads {
		ZeroScratchPad(&v.ScratchPads[i])
	}
}
