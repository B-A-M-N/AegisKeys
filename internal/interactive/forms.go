// Package interactive provides Huh form builders for the workflows that need
// user input beyond simple flags: adding/editing keys and providers. Both the
// TUI and the CLI reuse these forms so behavior stays consistent.
package interactive

import (
	"fmt"

	"charm.land/huh/v2"
)

// KeyForm captures the inputs needed to add or edit an API key record.
type KeyForm struct {
	ProviderSlug string
	Label        string
	Secret       string
	Tags         string
}

// RunKeyAdd presents a key-add form. providers is the list of available
// provider slugs to choose from.
func RunKeyAdd(providers []string) (KeyForm, error) {
	var f KeyForm

	opts := make([]huh.Option[string], 0, len(providers))
	for _, p := range providers {
		opts = append(opts, huh.NewOption(p, p))
	}

	var tags string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Provider").
				Value(&f.ProviderSlug).
				Options(opts...).
				Filtering(true),
			huh.NewInput().
				Title("Label").
				Value(&f.Label).
				Placeholder("e.g. main"),
			huh.NewInput().
				Title("Secret").
				Value(&f.Secret).
				Placeholder("sk-...").
				EchoMode(huh.EchoModePassword),
			huh.NewInput().
				Title("Tags").
				Value(&tags).
				Placeholder("comma-separated (optional)"),
		),
	).Run()
	f.Tags = tags
	return f, err
}

// RunKeyEdit pre-fills a key-edit form from existing values.
func RunKeyEdit(existing KeyForm) (KeyForm, error) {
	var f KeyForm
	f.ProviderSlug = existing.ProviderSlug

	var tags string
	if existing.Tags != "" {
		tags = existing.Tags
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Provider").
				Value(&f.ProviderSlug).
				Placeholder(existing.ProviderSlug),
			huh.NewInput().
				Title("Label").
				Value(&f.Label).
				Placeholder(existing.Label),
			huh.NewInput().
				Title("Secret").
				Value(&f.Secret).
				Placeholder("leave blank to keep current").
				EchoMode(huh.EchoModePassword),
			huh.NewInput().
				Title("Tags").
				Value(&tags).
				Placeholder(existing.Tags),
		),
	).Run()
	f.Tags = tags
	return f, err
}

// ProviderForm captures the inputs for adding or editing a provider.
type ProviderForm struct {
	Slug       string
	Name       string
	BaseURL    string
	EnvVar     string
	AuthHeader string
	Tags       string
	Notes      string
}

// RunProviderAdd presents a provider-add form.
func RunProviderAdd() (ProviderForm, error) {
	var f ProviderForm
	var tags string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Slug").Value(&f.Slug).Placeholder("openrouter").Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("slug is required")
				}
				return nil
			}),
			huh.NewInput().Title("Name").Value(&f.Name).Placeholder("OpenRouter"),
			huh.NewInput().Title("Base URL").Value(&f.BaseURL).Placeholder("https://openrouter.ai/api/v1"),
			huh.NewInput().Title("Env var").Value(&f.EnvVar).Placeholder("OPENROUTER_API_KEY"),
			huh.NewInput().Title("Auth header").Value(&f.AuthHeader).Placeholder("Authorization: Bearer ${KEY}"),
			huh.NewInput().Title("Tags").Value(&tags).Placeholder("comma-separated"),
			huh.NewInput().Title("Notes").Value(&f.Notes).Placeholder("(optional)"),
		),
	).Run()
	f.Tags = tags
	return f, err
}

// RunProviderEdit pre-fills a provider-edit form.
func RunProviderEdit(existing ProviderForm) (ProviderForm, error) {
	var f ProviderForm
	var tags string
	if existing.Tags != "" {
		tags = existing.Tags
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Slug").Value(&f.Slug).Placeholder(existing.Slug),
			huh.NewInput().Title("Name").Value(&f.Name).Placeholder(existing.Name),
			huh.NewInput().Title("Base URL").Value(&f.BaseURL).Placeholder(existing.BaseURL),
			huh.NewInput().Title("Env var").Value(&f.EnvVar).Placeholder(existing.EnvVar),
			huh.NewInput().Title("Auth header").Value(&f.AuthHeader).Placeholder(existing.AuthHeader),
			huh.NewInput().Title("Tags").Value(&tags).Placeholder(existing.Tags),
			huh.NewInput().Title("Notes").Value(&f.Notes).Placeholder(existing.Notes),
		),
	).Run()
	f.Tags = tags
	return f, err
}
