// Package proxy manages local proxy processes that applications may need
// to reach certain providers. Some coding agents cannot directly connect to
// APIs from restricted networks; a local proxy (SOCKS5, HTTP CONNECT, or
// a protocol-specific bridge) solves this. AegisKeys starts the proxy on
// demand and injects its address into the child environment.
package proxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Proxy describes a local proxy that can be started on demand.
type Proxy struct {
	// Name is a stable identifier (e.g. "claude-bridge", "corporate-socks5").
	Name string `json:"name"`

	// Address is the listen address in host:port form (e.g. "127.0.0.1:7890").
	Address string `json:"address"`

	// Type is the proxy protocol the application expects.
	// One of: "socks5", "http", "https".
	Type string `json:"type"`

	// StartCommand is the command to launch the proxy. If empty, AegisKeys
	// cannot auto-start it and will assume an external proxy is running.
	StartCommand string `json:"start_command,omitempty"`

	// StartArgs are arguments for the command.
	StartArgs []string `json:"start_args,omitempty"`

	// HealthCheck, if true, means AegisKeys should verify the proxy is
	// reachable before launching the child.
	HealthCheck bool `json:"health_check"`

	// EnvVar is the environment variable name that tells the child where
	// the proxy is (e.g. "HTTPS_PROXY"). If empty, no proxy env is set.
	EnvVar string `json:"env_var,omitempty"`

	// EnvValueTemplate is the value template. It may contain {address}
	// which is replaced with the proxy address.
	EnvValueTemplate string `json:"env_value_template,omitempty"`
}

// cleanEnv returns a minimal, safe environment for proxy processes.
// It includes only essential vars and strips secrets.
func cleanEnv() []string {
	keep := []string{"PATH", "HOME", "USER", "SHELL", "TERM", "LANG", "LC_ALL"}
	var env []string
	for _, k := range keep {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// EnvValue returns the environment variable value for this proxy, with
// {address} substituted.
func (p Proxy) EnvValue() string {
	if p.EnvValueTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(p.EnvValueTemplate, "{address}", p.Address)
}

// ProxyManager tracks running proxy processes.
type ProxyManager struct {
	mu      sync.Mutex
	running map[string]*exec.Cmd
	dataDir string
}

// NewManager creates a proxy manager that stores state in dataDir.
func NewManager(dataDir string) *ProxyManager {
	return &ProxyManager{
		running: make(map[string]*exec.Cmd),
		dataDir: dataDir,
	}
}

// IsReachable checks if a proxy at address is accepting connections.
func IsReachable(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// EnsureRunning starts the proxy if it is not already reachable.
// Returns the proxy address (possibly from an already-running external proxy).
func (m *ProxyManager) EnsureRunning(p Proxy) (string, error) {
	// If no auto-start command, just check reachability.
	if p.StartCommand == "" {
		if IsReachable(p.Address, 2*time.Second) {
			return p.Address, nil
		}
		return "", fmt.Errorf("proxy %s at %s is not reachable and has no auto-start command", p.Name, p.Address)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Already managed by us?
	if _, ok := m.running[p.Name]; ok {
		if IsReachable(p.Address, 1*time.Second) {
			return p.Address, nil
		}
		// Stale entry; clean up.
		delete(m.running, p.Name)
	}

	// External proxy already running?
	if IsReachable(p.Address, 1*time.Second) {
		return p.Address, nil
	}

	// Start it with a clean environment — proxy doesn't need parent secrets.
	cmd := exec.Command(p.StartCommand, p.StartArgs...)
	cmd.Env = cleanEnv()
	logPath := filepath.Join(m.dataDir, "tmp", p.Name+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return "", fmt.Errorf("proxy log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", fmt.Errorf("proxy log open: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return "", fmt.Errorf("proxy start: %w", err)
	}

	// The child inherited the log fd via Start(); the parent does not need it
	// open. Close it now to avoid leaking a file descriptor for the proxy's
	// lifetime.
	logFile.Close()

	m.running[p.Name] = cmd

	// Wait for it to become reachable.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if IsReachable(p.Address, 500*time.Millisecond) {
			return p.Address, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Timed out — kill it and reap.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	delete(m.running, p.Name)
	return "", fmt.Errorf("proxy %s did not become reachable within timeout", p.Name)
}

// Stop terminates a managed proxy. Proxies not started by AegisKeys are left alone.
func (m *ProxyManager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd, ok := m.running[name]
	if !ok {
		return nil // not managed by us
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	_ = cmd.Wait()
	delete(m.running, name)
	return nil
}

// StopAll terminates all managed proxies.
func (m *ProxyManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, cmd := range m.running {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		delete(m.running, name)
	}
}

// Running returns the names of proxies currently managed.
func (m *ProxyManager) Running() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for name := range m.running {
		out = append(out, name)
	}
	return out
}

// DefaultProxies returns common proxy configurations that users can adopt.
func DefaultProxies() []Proxy {
	return []Proxy{
		{
			Name:             "claude-bridge",
			Address:          "127.0.0.1:3456",
			Type:             "http",
			StartCommand:     "claude",
			StartArgs:        []string{"proxy"},
			HealthCheck:      true,
			EnvVar:           "ANTHROPIC_BASE_URL",
			EnvValueTemplate: "http://{address}",
		},
		{
			Name:             "corporate-socks5",
			Address:          "127.0.0.1:1080",
			Type:             "socks5",
			HealthCheck:      true,
			EnvVar:           "ALL_PROXY",
			EnvValueTemplate: "socks5://{address}",
		},
	}
}

// EnvForProxy returns the KEY=value string for injecting into the child env.
// Returns empty string if the proxy has no env var configured.
func EnvForProxy(p Proxy) string {
	if p.EnvVar == "" {
		return ""
	}
	return p.EnvVar + "=" + p.EnvValue()
}

// EnsureContext returns a context that will cancel (and thus stop managed
// proxies) when the parent context is done. Use this for long-running children.
func (m *ProxyManager) EnsureContext(p Proxy, ctx context.Context) (context.Context, context.CancelFunc, error) {
	addr, err := m.EnsureRunning(p)
	if err != nil {
		return ctx, func() {}, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	// When child context is done, we do NOT kill the proxy — it may be shared.
	_ = addr
	return childCtx, cancel, nil
}
