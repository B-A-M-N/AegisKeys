package proxy

import (
	"testing"
	"time"
)

func TestIsReachable_Unreachable(t *testing.T) {
	// Nothing should be listening on this port.
	if IsReachable("127.0.0.1:1", 500*time.Millisecond) {
		t.Error("expected unreachable")
	}
}

func TestProxy_EnvValue(t *testing.T) {
	p := Proxy{
		Address:          "127.0.0.1:7890",
		EnvVar:           "HTTPS_PROXY",
		EnvValueTemplate: "http://{address}",
	}
	if got := p.EnvValue(); got != "http://127.0.0.1:7890" {
		t.Errorf("EnvValue = %q", got)
	}
}

func TestProxy_EnvValue_NoTemplate(t *testing.T) {
	p := Proxy{Address: "127.0.0.1:7890"}
	if got := p.EnvValue(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestEnvForProxy(t *testing.T) {
	p := Proxy{
		Address:          "127.0.0.1:3456",
		EnvVar:           "ANTHROPIC_BASE_URL",
		EnvValueTemplate: "http://{address}",
	}
	got := EnvForProxy(p)
	want := "ANTHROPIC_BASE_URL=http://127.0.0.1:3456"
	if got != want {
		t.Errorf("EnvForProxy = %q, want %q", got, want)
	}
}

func TestDefaultProxies(t *testing.T) {
	proxies := DefaultProxies()
	if len(proxies) == 0 {
		t.Error("expected default proxies")
	}
	for _, p := range proxies {
		if p.Name == "" {
			t.Error("proxy missing name")
		}
		if p.Address == "" {
			t.Error("proxy missing address")
		}
	}
}
