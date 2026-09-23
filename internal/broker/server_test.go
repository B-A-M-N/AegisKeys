package broker

import (
	"aegiskeys/internal/secret"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestStatusHandler(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/app", CapabilityResolve)
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodGet, "/v1/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Protocol, Locked string
		LockedBool       bool `json:"locked"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if !strings.Contains(body, `"protocol":"v1"`) || !strings.Contains(body, `"locked":false`) {
		t.Fatalf("unexpected status body %s", body)
	}
}

func TestResolveAuthorizationAndResponse(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/authorized", CapabilityResolve)
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/resolve", `{"binding":"app/service"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if strings.Contains(body, "primary-secret-value") == false || !strings.Contains(body, "secondary-secret-value") {
		t.Fatalf("authorized response omitted components: %s", body)
	}
}

func TestSelectedComponentAccessIsExplicit(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/authorized", CapabilityResolve)
	// NewSession now reloads broker.json on every operation.
	meta, err := LoadBrokerFile(session.service.metaPath)
	if err != nil {
		t.Fatal(err)
	}
	meta.Bindings[0].ComponentAllowlist = []string{"primary"}
	if err := SaveBrokerFile(session.service.metaPath, meta); err != nil {
		t.Fatal(err)
	}
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/resolve", `{"binding":"app/service"}`)
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "secondary-secret-value") {
		t.Fatalf("secondary component escaped allowlist: status=%d body=%s", resp.StatusCode, body)
	}
}

func TestRevocationTakesEffectOnExistingKeepAliveClient(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/authorized", CapabilityResolve)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", session.listener.Addr().String())
	}}}
	post := func() int {
		req, _ := http.NewRequest(http.MethodPost, "http://broker/v1/resolve", strings.NewReader(`{"binding":"app/service"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := post(); code != http.StatusOK {
		t.Fatalf("initial resolve = %d", code)
	}
	meta, err := LoadBrokerFile(session.service.metaPath)
	if err != nil {
		t.Fatal(err)
	}
	meta.Grants[0].Enabled = false
	if err := SaveBrokerFile(session.service.metaPath, meta); err != nil {
		t.Fatal(err)
	}
	if code := post(); code != http.StatusForbidden {
		t.Fatalf("revoked keep-alive resolve = %d", code)
	}
}

func TestResolveDeniedWithoutSecretPolicy(t *testing.T) {
	session, _, _ := newTestSession(t, false, "/opt/authorized", CapabilityResolve)
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/resolve", `{"binding":"app/service"}`)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "access denied") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestResolveDeniedWithoutGrant(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/other")
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/resolve", `{"binding":"app/service"}`)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "access denied") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestRotateRequiresCapability(t *testing.T) {
	session, vaultPath, key := newTestSession(t, true, "/opt/provisioner", CapabilityResolve)
	resp, _ := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/rotate", `{"binding":"app/service","value":"new-secret"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("rotation without grant: status=%d", resp.StatusCode)
	}
	v, err := secret.LoadVaultByKey(vaultPath, key)
	if err != nil || v.Get("key_broker").Secret != "primary-secret-value" {
		t.Fatalf("unauthorized rotate changed vault: err=%v", err)
	}
}

func TestRotateAuthorizedChangesOnlyTarget(t *testing.T) {
	session, vaultPath, key := newTestSession(t, true, "/opt/provisioner", CapabilityResolve, CapabilityRotate)
	resp, body := unixJSON(t, session.listener.Addr().String(), http.MethodPost, "/v1/rotate", `{"binding":"app/service","value":"rotated-secret"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	v, err := secret.LoadVaultByKey(vaultPath, key)
	if err != nil {
		t.Fatal(err)
	}
	rec := v.Get("key_broker")
	if rec == nil || rec.Secret != "rotated-secret" || rec.Label != "Primary" {
		t.Fatalf("rotation changed wrong fields: %+v", rec)
	}
	if len(v.Keys) != 1 {
		t.Fatalf("vault key count changed: %d", len(v.Keys))
	}
}

func TestProtocolValidation(t *testing.T) {
	session, _, _ := newTestSession(t, true, "/opt/app", CapabilityResolve, CapabilityRotate)
	addr := session.listener.Addr().String()
	cases := []struct {
		method, path, body string
		code               int
	}{
		{http.MethodGet, "/v1/resolve", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/resolve", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/resolve", `{"binding":"","value":"x"}`, http.StatusBadRequest},
		{http.MethodPost, "/v1/resolve", `{"binding":"x","unknown":1}`, http.StatusBadRequest},
		{http.MethodPost, "/v1/rotate", `{"binding":"x"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		resp, _ := unixJSON(t, addr, c.method, c.path, c.body)
		if resp.StatusCode != c.code {
			t.Fatalf("%s %s %s: got %d want %d", c.method, c.path, c.body, resp.StatusCode, c.code)
		}
	}
}

func TestRequestDecoderRejectsUnknownFields(t *testing.T) {
	var req Request
	dec := json.NewDecoder(strings.NewReader(`{"binding":"x","unknown":1}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err == nil {
		t.Fatal("expected unknown request field to be rejected")
	}
}
