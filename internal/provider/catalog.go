package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"
)

var newCatalogHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

// RefreshModels fetches a provider's dynamic model catalog. It supports
// OpenAI-compatible /models responses and the common Gemini models response.
func RefreshModels(ctx context.Context, p Provider, apiKey string) ([]ProviderModel, error) {
	p.Normalize()
	if err := p.ValidateStrict(); err != nil {
		return nil, fmt.Errorf("provider metadata invalid: %w", err)
	}
	url := p.ModelRefreshURL()
	if url == "" {
		return nil, fmt.Errorf("provider %s has no models endpoint", p.Slug)
	}
	parsed, err := urlpkg.Parse(url)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid models endpoint")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackModelHost(parsed.Hostname())) {
		return nil, fmt.Errorf("models endpoint must use https unless loopback")
	}
	if p.NeedsKey() && !p.AllowCredentialOrigin && !sameApprovedOrigin(parsed, p.CanonicalBaseURL()) {
		return nil, fmt.Errorf("authenticated models endpoint origin %q is not approved for provider base origin", parsed.Scheme+"://"+parsed.Host)
	}
	if p.NeedsKey() && !p.AllowCredentialOrigin && parsed.Path != "" && !strings.HasPrefix(parsed.Path, "/") {
		return nil, fmt.Errorf("invalid models endpoint path")
	}
	if url == "" {
		url = p.Catalog.RefreshURL
	}
	if url == "" {
		url = p.ModelPolicy.RefreshURL
	}
	if url == "" {
		base := strings.TrimRight(p.CanonicalBaseURL(), "/")
		if base == "" {
			return nil, fmt.Errorf("provider %s has no models endpoint", p.Slug)
		}
		url = base + "/models"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.NeedsKey() && apiKey == "" {
		return nil, fmt.Errorf("provider %s requires an API key to refresh models", p.Slug)
	}
	if p.Auth.Type == "query" {
		return nil, fmt.Errorf("query authentication is not permitted for model catalog requests")
	}
	applyAuth(req, p, apiKey)

	client := newCatalogHTTPClient()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		return validateModelRedirect(next.URL, via, p.CanonicalBaseURL(), p.AllowCredentialOrigin)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("models endpoint returned %s", res.Status)
	}

	var raw struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	const maxModelsResponse = 4 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxModelsResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}
	if len(body) > maxModelsResponse {
		return nil, fmt.Errorf("models response exceeds %d bytes", maxModelsResponse)
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}

	if len(raw.Data)+len(raw.Models) > 10000 {
		return nil, fmt.Errorf("models response contains too many models")
	}
	models := make([]ProviderModel, 0, len(raw.Data)+len(raw.Models))
	for _, item := range raw.Data {
		if item.ID != "" {
			if len(item.ID) > 512 {
				return nil, fmt.Errorf("model ID exceeds length limit")
			}
			models = append(models, ProviderModel{ID: item.ID})
		}
	}
	for _, item := range raw.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if id == "" {
			continue
		}
		if len(id) > 512 || len(item.DisplayName) > 512 {
			return nil, fmt.Errorf("model field exceeds length limit")
		}
		models = append(models, ProviderModel{
			ID:          id,
			Name:        item.DisplayName,
			ContextSize: item.InputTokenLimit,
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("models endpoint returned no usable models")
	}
	return models, nil
}

func validateModelRedirect(next *urlpkg.URL, via []*http.Request, base string, allowCredentialOrigin bool) error {
	if len(via) >= 3 {
		return fmt.Errorf("blocked models redirect chain")
	}
	if next.Scheme != "https" && !(next.Scheme == "http" && isLoopbackModelHost(next.Hostname())) {
		return fmt.Errorf("blocked insecure models redirect")
	}
	if !sameApprovedOrigin(next, base) && !allowCredentialOrigin {
		return fmt.Errorf("blocked cross-origin models redirect")
	}
	return nil
}

func sameApprovedOrigin(a *urlpkg.URL, base string) bool {
	if base == "" {
		return true
	}
	b, err := urlpkg.Parse(base)
	if err != nil || b.Host == "" {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func isLoopbackModelHost(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func applyAuth(req *http.Request, p Provider, apiKey string) {
	if apiKey == "" {
		return
	}
	switch p.Auth.Type {
	case "bearer":
		prefix := p.Auth.Prefix
		if prefix == "" {
			prefix = "Bearer "
		}
		header := p.Auth.HeaderName
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, prefix+apiKey)
	case "header":
		header := p.Auth.HeaderName
		if header != "" {
			req.Header.Set(header, apiKey)
		}
	case "query":
		q := req.URL.Query()
		q.Set("key", apiKey)
		req.URL.RawQuery = q.Encode()
	}
}
