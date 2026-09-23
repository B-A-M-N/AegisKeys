//go:build linux

package broker

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// LinuxPeerResolver reads SO_PEERCRED and pins the resolved executable.
type LinuxPeerResolver struct {
	// UID must match the accepted peer's effective UID. It is normally the
	// broker process owner and prevents an inbound descriptor forged by
	// another local user from acquiring this user's identity.
	UID             int
	HashExecutables bool
}

// Resolve implements PeerResolver for Linux Unix-domain sockets.
func (r LinuxPeerResolver) Resolve(c net.Conn) (PeerIdentity, error) {
	syscaller, supported := c.(interface {
		SyscallConn() syscall.RawConn
	})
	if !supported {
		return PeerIdentity{}, errors.New("peer credentials require a Unix socket")
	}
	rawConn := syscaller.SyscallConn()
	var cred *unix.Ucred
	var controlErr error
	if err := rawConn.Control(func(fd uintptr) {
		cred, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return PeerIdentity{}, err
	}
	if controlErr != nil {
		return PeerIdentity{}, controlErr
	}
	return r.fromUcred(cred)
}
func (r LinuxPeerResolver) fromUcred(cred *unix.Ucred) (PeerIdentity, error) {
	if cred == nil {
		return PeerIdentity{}, errors.New("peer credentials unavailable")
	}
	if int(cred.Uid) != r.UID {
		return PeerIdentity{}, ErrAccessDenied
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", cred.Pid))
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("resolve peer executable: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("canonicalize peer executable: %w", err)
	}
	identity := PeerIdentity{PID: int(cred.Pid), UID: int(cred.Uid), GID: int(cred.Gid), ExecutablePath: canonical}
	if r.HashExecutables {
		hash, err := HashExecutable(canonical)
		if err != nil {
			return PeerIdentity{}, err
		}
		identity.ExecutableHash = hash
	}
	return identity, nil
}
