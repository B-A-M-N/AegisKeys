// Package client is the public, broker-only AegisKeys credential client.
// It never reads vault.enc, loads the master key, or changes grants.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrAccessDenied identifies a broker authorization denial. Callers can use
// errors.Is to trigger an explicit operator approval flow.
var ErrAccessDenied = errors.New("aegiskeys: access denied")

// Credential is the subset of a broker-resolved credential exposed to clients.
type Credential struct {
	Binding    string            `json:"binding"`
	Kind       string            `json:"kind,omitempty"`
	EnvVar     string            `json:"env_var,omitempty"`
	Value      string            `json:"value,omitempty"`
	Components map[string]string `json:"components,omitempty"`
}

// Client talks only to the protected local broker socket.
type Client struct {
	http *http.Client
}

// Connect creates a client for an AegisKeys config directory.
func Connect(ctx context.Context, configDir string) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	if configDir == "" {
		configDir = filepath.Join(home, ".config", "aegiskeys")
	}
	socket := filepath.Join(configDir, "run", "broker.sock")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 5 * time.Second}}, nil
}

// Resolve asks the broker for one explicitly approved binding.
func (c *Client) Resolve(ctx context.Context, binding string) (Credential, error) {
	if c == nil || c.http == nil {
		return Credential{}, errors.New("nil broker client")
	}
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	body, err := json.Marshal(map[string]string{"binding": binding})
	if err != nil {
		return Credential{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://aegiskeys.local/v1/resolve", bytes.NewReader(body))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Credential{}, ErrAccessDenied
	}
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("broker resolve failed: %s", resp.Status)
	}
	var credential Credential
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&credential); err != nil {
		return Credential{}, fmt.Errorf("decode broker response: %w", err)
	}
	return credential, nil
}
