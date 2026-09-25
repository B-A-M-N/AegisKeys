package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestRefreshModelsRejectsInsecureCrossOriginBeforeSendingKey(t *testing.T) {
	p := Provider{Name: "x", Slug: "x", EnvVar: "X_API_KEY", BaseURL: "https://api.example.com/v1", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: "http://attacker.invalid/models"}}
	if _, err := RefreshModels(context.Background(), p, "SENSITIVE_KEY_VALUE_123"); err == nil {
		t.Fatal("insecure refresh URL accepted")
	}
}

func TestRefreshModelsPinsRedirectOrigin(t *testing.T) {
	if os.Getenv("AEGISKEYS_ALLOW_TCP_TESTS") == "" {
		t.Skip("sandbox denies TCP listener; set on a network-enabled host")
	}
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("cross-origin redirect followed") }))
	defer attacker.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, attacker.URL, http.StatusFound) }))
	defer upstream.Close()
	p := Provider{Name: "x", Slug: "x", EnvVar: "X_API_KEY", BaseURL: "https://api.example.com/v1", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: upstream.URL + "/models"}}
	if _, err := RefreshModels(context.Background(), p, "SENSITIVE_KEY_VALUE_123"); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
}

func TestRefreshModelsRejectsCrossOriginCredentialRecipient(t *testing.T) {
	p := Provider{Name: "x", Slug: "x", EnvVar: "X_API_KEY", BaseURL: "https://api.example.com/v1", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: "https://attacker.example/models"}}
	if _, err := RefreshModels(context.Background(), p, "secret"); err == nil {
		t.Fatal("cross-origin credential recipient accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefreshModelsRejectsHTTPSDowngradeRedirect(t *testing.T) {
	var initialCredential bool
	var downgradeFollowed bool
	oldClient := newCatalogHTTPClient
	newCatalogHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme == "http" {
				downgradeFollowed = true
				return nil, fmt.Errorf("unexpected insecure follow-up request")
			}
			initialCredential = r.Header.Get("Authorization") == "Bearer SENSITIVE_KEY_VALUE_123"
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"http://api.example.com/models"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    r,
			}, nil
		})}
	}
	t.Cleanup(func() { newCatalogHTTPClient = oldClient })
	p := Provider{
		Name: "x", Slug: "x", BaseURL: "https://api.example.com",
		Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"},
		Catalog: ModelCatalogSpec{RefreshURL: "https://api.example.com/models"}, AllowCredentialOrigin: true,
	}
	if _, err := RefreshModels(context.Background(), p, "SENSITIVE_KEY_VALUE_123"); err == nil {
		t.Fatal("HTTPS downgrade redirect accepted")
	}
	if !initialCredential {
		t.Fatal("test did not exercise an authenticated initial request")
	}
	if downgradeFollowed {
		t.Fatal("credential-bearing downgrade request reached the destination")
	}
}

func TestModelRedirectPolicyRejectsDowngradeAndLongChain(t *testing.T) {
	downgrade, _ := url.Parse("http://api.example.com/models")
	if err := validateModelRedirect(downgrade, []*http.Request{{}}, "https://api.example.com", true); err == nil {
		t.Fatal("downgrade policy accepted HTTP redirect")
	}
	if err := validateModelRedirect(downgrade, []*http.Request{{}, {}, {}}, "https://api.example.com", true); err == nil {
		t.Fatal("redirect policy accepted an overlong chain")
	}
}

func TestQueryAuthenticationRequiresIndependentConsent(t *testing.T) {
	p := Provider{Name: "x", Slug: "x", BaseURL: "https://api.example.com", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "query", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: "https://api.example.com/models"}}
	if err := p.ValidateStrict(); err == nil {
		t.Fatal("query auth accepted without consent")
	}
	p.AllowQueryAuth = true
	if err := p.ValidateStrict(); err != nil {
		t.Fatalf("explicit query consent rejected: %v", err)
	}
	if p.AllowCredentialOrigin {
		t.Fatal("query consent incorrectly set credential-origin consent")
	}
}
