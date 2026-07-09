package provider

import "testing"

func TestResolveEndpointSubstitution(t *testing.T) {
	cases := []struct {
		name   string
		tpl    string
		fields map[string]string
		want   string
	}{
		{"azure", "https://{resource}.openai.azure.com", map[string]string{"resource": "my-resource"}, "https://my-resource.openai.azure.com"},
		{"bedrock", "https://bedrock-runtime.{region}.amazonaws.com", map[string]string{"region": "eu-west-1"}, "https://bedrock-runtime.eu-west-1.amazonaws.com"},
		{"no template falls back", "", map[string]string{"x": "y"}, "https://base.example.com"},
		{"unmatched left intact", "https://{missing}.example.com", map[string]string{}, "https://{missing}.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Provider{Endpoints: EndpointSpec{BaseURL: "https://base.example.com", URLTemplate: c.tpl}}
			got := p.ResolveEndpoint(c.fields)
			if got != c.want {
				t.Errorf("ResolveEndpoint = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAzureCatalogDeclaresSetup(t *testing.T) {
	p := findDefault("azure-openai")
	if p == nil {
		t.Fatal("azure-openai provider missing")
	}
	keys := map[string]SetupParam{}
	for _, sp := range p.Setup {
		keys[sp.Key] = sp
	}
	for _, want := range []string{"resource", "deployment", "api_version"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("azure setup missing required param %q", want)
		}
	}
	// api-version must declare its env var and a default.
	if keys["api_version"].EnvVar != "AZURE_OPENAI_API_VERSION" {
		t.Errorf("api_version EnvVar = %q", keys["api_version"].EnvVar)
	}
	if keys["api_version"].Default == "" {
		t.Errorf("api_version should declare a default")
	}
	// resource maps to the endpoint env var.
	if keys["resource"].EnvVar != "AZURE_OPENAI_ENDPOINT" {
		t.Errorf("resource EnvVar = %q", keys["resource"].EnvVar)
	}
}

func TestBedrockCatalogDeclaresSetup(t *testing.T) {
	p := findDefault("bedrock")
	if p == nil {
		t.Fatal("bedrock provider missing")
	}
	if p.Auth.Type != "aws" {
		t.Errorf("bedrock auth type = %q, want aws", p.Auth.Type)
	}
	keys := map[string]SetupParam{}
	for _, sp := range p.Setup {
		keys[sp.Key] = sp
	}
	sak, ok := keys["secret_access_key"]
	if !ok {
		t.Fatal("bedrock setup missing secret_access_key")
	}
	if !sak.Secret {
		t.Errorf("secret_access_key must be marked Secret")
	}
	if sak.EnvVar != "AWS_SECRET_ACCESS_KEY" {
		t.Errorf("secret_access_key EnvVar = %q", sak.EnvVar)
	}
	region := keys["region"]
	if region.EnvVar != "AWS_REGION" {
		t.Errorf("region EnvVar = %q", region.EnvVar)
	}
	if region.Default == "" {
		t.Errorf("region should declare a default")
	}
}

func findDefault(slug string) *Provider {
	for i := range DefaultProviders() {
		if DefaultProviders()[i].Slug == slug {
			return &DefaultProviders()[i]
		}
	}
	return nil
}
