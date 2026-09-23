package broker

import "net"

// PeerIdentity is the transport-neutral authenticated caller description.
type PeerIdentity struct {
	PID            int
	UID            int
	GID            int
	ExecutablePath string
	ExecutableHash string
	VaultKey       [32]byte
}

// PeerResolver obtains caller identity from an accepted connection.
type PeerResolver interface {
	Resolve(net.Conn) (PeerIdentity, error)
}

// LocalResolver is used by out-of-process integration tests only. Production
// listener construction always injects the platform credential resolver.
type LocalResolver struct{ Identity PeerIdentity }

func (r LocalResolver) Resolve(net.Conn) (PeerIdentity, error) { return r.Identity, nil }
