package broker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Server limits are deliberately conservative for a same-user local service.
const (
	maxRequests    = 16
	maxRequestBody = 64 * 1024
	readTimeout    = 5 * time.Second
	writeTimeout   = 5 * time.Second
	idleTimeout    = 30 * time.Second
)

// Session owns the broker listener and the derived vault key. It intentionally
// retains only the key, not a long-lived decrypted vault.

// identityListener resolves peer identity once per accepted connection. The
// identity is attached to that connection, never indexed by RemoteAddr: Unix
// sockets are unnamed and simultaneous clients can share one address.
type identityListener struct {
	net.Listener
	peer PeerResolver
}

type identityConn struct {
	net.Conn
	peer PeerIdentity
}

func (l *identityListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	id, resolveErr := l.peer.Resolve(c)
	if resolveErr != nil {
		_ = c.Close()
		return nil, resolveErr
	}
	return &identityConn{Conn: c, peer: id}, nil
}

type Session struct {
	listener *identityListener
	server   *http.Server
	peer     PeerResolver
	service  *Service
	audit    auditLogger

	mu       sync.Mutex
	locked   bool
	vaultKey [32]byte
	inflight chan struct{}
	closed   bool
}

type auditLogger interface {
	Log(event, result string, metadata map[string]string)
}

// Listen creates the runtime directory and binds a safely validated
// Unix-domain socket. It refuses attacker-plausible preexisting paths.
func Listen(configDir, socketPath string, peer PeerResolver) (net.Listener, error) {
	if peer == nil {
		return nil, errors.New("nil peer resolver")
	}
	runtimeDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(runtimeDir, 0700); err != nil {
		return nil, err
	}
	if err := validateRuntimeDir(runtimeDir); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing unsafe socket path %s: not a socket", socketPath)
		}
		if !sameOwner(fileUID(info)) {
			return nil, fmt.Errorf("refusing socket %s: not owned by current user", socketPath)
		}
		// A stale socket from a dead prior process is the only removable type.
		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func validateRuntimeDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid runtime directory %s", dir)
	}
	if !sameOwner(fileUID(info)) {
		return fmt.Errorf("runtime directory %s is not owned by current user", dir)
	}
	if info.Mode().Perm()&(0o022) != 0 {
		return fmt.Errorf("runtime directory %s must not be group/world writable", dir)
	}
	return nil
}

func sameOwner(uid uint32) bool { return uid == currentUID() }

// fileUID extracts the owner on Unix systems. On unsupported platforms, a
// sentinel never matches the current user, failing closed.
func fileUID(info os.FileInfo) uint32 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Uid
	}
	return ^uint32(0)
}

// NewSession constructs the HTTP session. It retains the supplied derived key
// and no decrypted vault.
func NewSession(listener net.Listener, meta *File, vaultPath string, key [32]byte, peer PeerResolver, audit auditLogger) (*Session, error) {
	if peer == nil {
		return nil, errors.New("nil peer resolver")
	}
	service, err := NewService(meta, vaultPath)
	if err != nil {
		return nil, err
	}
	s := &Session{
		listener: &identityListener{Listener: listener, peer: peer},
		peer:     peer,
		service:  service,
		audit:    audit,
		vaultKey: key,
		inflight: make(chan struct{}, maxRequests),
	}
	// Production sessions always authorize from persisted broker.json.
	service.metaPath = filepath.Join(filepath.Dir(vaultPath), "broker.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/resolve", s.handleResolve)
	mux.HandleFunc("/v1/rotate", s.handleRotate)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A short bounded wait sheds excess same-user load instead of letting
		// concurrent requests pin vault decryption memory indefinitely.
		select {
		case s.inflight <- struct{}{}:
			defer func() { <-s.inflight }()
		case <-time.After(2 * time.Second):
			writeError(w, http.StatusTooManyRequests, "request invalid")
			return
		}
		mux.ServeHTTP(w, r)
	})
	s.server = &http.Server{
		Handler: handler,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if ic, ok := c.(*identityConn); ok {
				return context.WithValue(ctx, peerIdentityKey{}, ic.peer)
			}
			return ctx
		},
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}
	return s, nil
}

// Serve blocks until the listener is closed.
func (s *Session) Serve() error { return s.server.Serve(s.listener) }

// Close shuts down the server and best-effort zeroes the derived key.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	key := s.vaultKey
	s.vaultKey = [32]byte{}
	s.mu.Unlock()
	_ = s.server.Close()
	for i := range key {
		key[i] = 0
	}
	s.audit.Log("broker_stopped", "ok", nil)
	return nil
}

// Lock zeroes the retained derived key while leaving the socket listening.
func (s *Session) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.vaultKey {
		s.vaultKey[i] = 0
	}
	s.locked = true
	s.audit.Log("broker_locked", "ok", nil)
}

// Locked reports whether the session has zeroed its key.
func (s *Session) Locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locked
}

func (s *Session) key() ([32]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return [32]byte{}, false
	}
	return s.vaultKey, true
}

func (s *Session) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w)
		return
	}
	_, unlocked := s.key()
	writeJSON(w, unlockedStatus{Protocol: ProtocolVersion, Locked: !unlocked})
}

type unlockedStatus struct {
	Protocol string `json:"protocol"`
	Locked   bool   `json:"locked"`
}

func (s *Session) handleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}
	var req Request
	if !s.decodeRequest(w, r, &req, false) {
		return
	}
	peer, ok := s.authorize(w, r, req.Binding, CapabilityResolve, "resolve")
	if !ok {
		return
	}
	key, unlocked := s.key()
	if !unlocked {
		s.lockedResponse(w, "resolve", peer)
		return
	}
	peer.VaultKey = key
	result, err := s.service.Resolve(req.Binding, peer)
	if err != nil {
		s.serviceError(w, "resolve", peer, err)
		return
	}
	writeJSON(w, result)
	s.audit.Log("credential_resolved", "ok", map[string]string{"binding_ref": auditBindingRef(req.Binding), "client_executable": peer.ExecutablePath})
}

func (s *Session) handleRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w)
		return
	}
	var req Request
	if !s.decodeRequest(w, r, &req, true) {
		return
	}
	peer, ok := s.authorize(w, r, req.Binding, CapabilityRotate, "rotate")
	if !ok {
		return
	}
	key, unlocked := s.key()
	if !unlocked {
		s.lockedResponse(w, "rotate", peer)
		return
	}
	peer.VaultKey = key
	s.audit.Log("credential_rotation_requested", "ok", map[string]string{"binding_ref": auditBindingRef(req.Binding), "client_executable": peer.ExecutablePath})
	if err := s.service.Rotate(req, peer); err != nil {
		s.serviceError(w, "rotate", peer, err)
		return
	}
	writeJSON(w, struct{}{})
	s.audit.Log("credential_rotated", "ok", map[string]string{"binding_ref": auditBindingRef(req.Binding), "client_executable": peer.ExecutablePath})
}

type peerIdentityKey struct{}

func (s *Session) authorize(w http.ResponseWriter, r *http.Request, binding string, capability Capability, operation string) (PeerIdentity, bool) {
	peer, known := r.Context().Value(peerIdentityKey{}).(PeerIdentity)
	if !known {
		writeError(w, http.StatusForbidden, "access denied")
		s.audit.Log("credential_access_denied", "denied", map[string]string{
			"operation": operation, "binding_ref": auditBindingRef(binding),
		})
		return PeerIdentity{}, false
	}
	// The resolver never supplies the key; authorization precedes vault load.
	peer.VaultKey = [32]byte{}
	s.audit.Log("credential_access_requested", "pending", map[string]string{
		"binding_ref": auditBindingRef(binding), "operation": string(capability), "client_executable": peer.ExecutablePath,
	})
	return peer, true
}

func (s *Session) decodeRequest(w http.ResponseWriter, r *http.Request, dst *Request, allowValue bool) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "request invalid")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.Decode(&struct{}{}) == nil {
		writeError(w, http.StatusBadRequest, "request invalid")
		return false
	}
	if !ValidBindingName(dst.Binding) || (!allowValue && dst.Value != "") {
		writeError(w, http.StatusBadRequest, "request invalid")
		return false
	}
	return true
}

func auditBindingRef(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func (s *Session) methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "request invalid")
}

func (s *Session) lockedResponse(w http.ResponseWriter, operation string, peer PeerIdentity) {
	writeError(w, http.StatusLocked, "vault locked")
	s.audit.Log("credential_access_denied", "locked", map[string]string{
		"operation": operation, "client_executable": peer.ExecutablePath,
	})
}

func (s *Session) serviceError(w http.ResponseWriter, operation string, peer PeerIdentity, err error) {
	switch {
	case errors.Is(err, ErrAccessDenied):
		writeError(w, http.StatusForbidden, "access denied")
	case errors.Is(err, ErrBindingUnavailable):
		writeError(w, http.StatusNotFound, "binding unavailable")
	case errors.Is(err, ErrLocked):
		writeError(w, http.StatusLocked, "vault locked")
	default:
		writeError(w, http.StatusBadRequest, "request invalid")
	}
	if errors.Is(err, ErrAccessDenied) {
		s.audit.Log("credential_access_denied", "denied", map[string]string{
			"operation": operation, "client_executable": peer.ExecutablePath,
		})
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, code int, generic string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": generic})
}

func currentUID() uint32 {
	if u, err := user.Current(); err == nil {
		var uid uint32
		fmt.Sscanf(u.Uid, "%d", &uid)
		return uid
	}
	return ^uint32(0)
}
