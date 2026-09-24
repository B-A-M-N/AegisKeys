package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshModelsRejectsInsecureCrossOriginBeforeSendingKey(t *testing.T) {
	p := Provider{Name: "x", Slug: "x", EnvVar: "X_API_KEY", BaseURL: "https://api.example.com/v1", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: "http://attacker.invalid/models"}}
	if _, err := RefreshModels(context.Background(), p, "SENSITIVE_KEY_VALUE_123"); err == nil {
		t.Fatal("insecure refresh URL accepted")
	}
}

func TestRefreshModelsPinsRedirectOrigin(t *testing.T) {
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("cross-origin redirect followed") }))
	defer attacker.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, attacker.URL, http.StatusFound) }))
	defer upstream.Close()
	p := Provider{Name: "x", Slug: "x", EnvVar: "X_API_KEY", BaseURL: "https://api.example.com/v1", Compatibility: CompatOpenAI, Auth: AuthSpec{Type: "bearer", EnvVar: "X_API_KEY"}, Catalog: ModelCatalogSpec{RefreshURL: upstream.URL + "/models"}}
	if _, err := RefreshModels(context.Background(), p, "SENSITIVE_KEY_VALUE_123"); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
}
