package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"
)

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
	applyAuth(req, p, apiKey)

	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 || next.URL.Host != parsed.Host {
			return fmt.Errorf("blocked cross-origin models redirect")
		}
		return nil
	}}
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
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}

	models := make([]ProviderModel, 0, len(raw.Data)+len(raw.Models))
	for _, item := range raw.Data {
		if item.ID != "" {
			models = append(models, ProviderModel{ID: item.ID})
		}
	}
	for _, item := range raw.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if id == "" {
			continue
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
