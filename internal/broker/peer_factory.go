package broker

import "os"

// NewPeerResolver returns the platform's fail-closed peer authenticator.
// Unsupported platforms compile and leave the rest of AegisKeys usable, but
// `broker serve` cannot authorize clients there.
func NewPeerResolver() PeerResolver { return newPlatformPeerResolver(os.Getuid(), true) }
