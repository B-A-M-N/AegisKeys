//go:build !linux

package broker

import (
	"errors"
	"net"
)

// UnsupportedPeerResolver fails closed on platforms without an implemented
// peer-credential resolver.
type UnsupportedPeerResolver struct{}

// newPlatformPeerResolver keeps non-Linux binaries compiling while broker
// authorization remains unavailable.
func newPlatformPeerResolver(int, bool) PeerResolver { return UnsupportedPeerResolver{} }

func (UnsupportedPeerResolver) Resolve(net.Conn) (PeerIdentity, error) {
	return PeerIdentity{}, errors.New("peer authentication is not supported on this platform")
}
