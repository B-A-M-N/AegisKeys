//go:build linux

package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
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

// newPlatformPeerResolver is the Linux implementation of the public factory.
func newPlatformPeerResolver(uid int, hashExecutables bool) PeerResolver {
	return LinuxPeerResolver{UID: uid, HashExecutables: hashExecutables}
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
	procExe := fmt.Sprintf("/proc/%d/exe", cred.Pid)
	exeFile, err := os.Open(procExe)
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("resolve peer executable: %w", err)
	}
	defer exeFile.Close()
	exe, err := os.Readlink(procExe)
	if err != nil {
		_ = exeFile.Close()
		return PeerIdentity{}, fmt.Errorf("resolve peer executable: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("canonicalize peer executable: %w", err)
	}
	// /proc/<pid>/exe is a descriptor-backed reference to the inode the peer
	// is actually executing. Keep that inode pinned while comparing the
	// separately resolved path and hashing the same file description.
	procInfo, err := exeFile.Stat()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("stat peer executable: %w", err)
	}
	pathFile, err := os.Open(canonical)
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("open resolved peer executable: %w", err)
	}
	defer pathFile.Close()
	pathInfo, err := pathFile.Stat()
	if err != nil {
		return PeerIdentity{}, fmt.Errorf("stat resolved peer executable: %w", err)
	}
	if !sameFileIdentity(procInfo, pathInfo) {
		return PeerIdentity{}, errors.New("executable identity changed during peer verification")
	}
	identity := PeerIdentity{PID: int(cred.Pid), UID: int(cred.Uid), GID: int(cred.Gid), ExecutablePath: canonical}
	if r.HashExecutables {
		h := sha256.New()
		if _, err := io.Copy(h, exeFile); err != nil {
			return PeerIdentity{}, err
		}
		identity.ExecutableHash = hex.EncodeToString(h.Sum(nil))
	}
	return identity, nil
}

func sameFileIdentity(a, b os.FileInfo) bool {
	return fileIdentityKey(a) == fileIdentityKey(b)
}

func fileIdentityKey(info os.FileInfo) string {
	// Stat_t is Linux-specific here; use the syscall representation without
	// relying on a pathname lookup. This helper is kept separate for tests.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("%T:%v", info.Sys(), info.Sys())
	}
	return strconv.FormatUint(uint64(stat.Dev), 10) + ":" + strconv.FormatUint(uint64(stat.Ino), 10)
}
