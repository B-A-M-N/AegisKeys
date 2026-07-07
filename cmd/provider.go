package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/interactive"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/redact"
)

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage provider metadata",
}

var providerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		if len(reg.Providers) == 0 {
			fmt.Println("No providers configured. Run `aegiskeys init` to seed defaults.")
			return nil
		}
		fmt.Printf("%-16s %-24s %s\n", "SLUG", "NAME", "ENV VAR")
		for _, p := range reg.Providers {
			fmt.Printf("%-16s %-24s %s\n", redactProviderOutput(p.Slug), redactProviderOutput(p.Name), redactProviderOutput(p.EnvVar))
		}
		return nil
	},
}

var providerInspectCmd = &cobra.Command{
	Use:   "inspect <slug>",
	Short: "Show details for a provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		p := reg.Find(args[0])
		if p == nil {
			return fmt.Errorf("no provider with slug %q", args[0])
		}
		if err := p.ValidateStrict(); err != nil {
			fmt.Printf("Warning: provider metadata failed strict validation: %s\n", redactProviderOutput(err.Error()))
		}
		fmt.Printf("Name:       %s\n", redactProviderOutput(p.Name))
		fmt.Printf("Slug:       %s\n", redactProviderOutput(p.Slug))
		fmt.Printf("Base URL:   %s\n", redactProviderOutput(p.BaseURL))
		fmt.Printf("Env var:    %s\n", redactProviderOutput(p.EnvVar))
		fmt.Printf("Auth:       %s\n", redactProviderOutput(p.AuthHeader))
		if len(p.Models) > 0 {
			names := make([]string, 0, len(p.Models))
			for _, m := range p.Models {
				if m.Name != "" {
					names = append(names, m.Name)
				} else {
					names = append(names, m.ID)
				}
			}
			fmt.Printf("Models:     %s\n", strings.Join(names, ", "))
		}
		if len(p.Tags) > 0 {
			fmt.Printf("Tags:       %s\n", strings.Join(p.Tags, ", "))
		}
		if len(p.ExtraEnv) > 0 {
			fmt.Println("Extra env:")
			for k, v := range p.ExtraEnv {
				fmt.Printf("  %s=%s\n", redactProviderOutput(k), redactProviderOutput(v))
			}
		}
		if p.Notes != "" {
			fmt.Printf("Notes:      %s\n", redactProviderOutput(p.Notes))
		}
		return nil
	},
}

var providerModelsCmd = &cobra.Command{
	Use:   "models <slug>",
	Short: "List known models for a provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		p := reg.Find(args[0])
		if p == nil {
			return fmt.Errorf("no provider with slug %q", args[0])
		}
		if len(p.Models) == 0 {
			fmt.Printf("No cached model catalog for %s. Use manual model entry or refresh dynamic catalog when implemented.\n", p.Slug)
			return nil
		}
		fmt.Printf("%-36s %-24s %s\n", "ID", "NAME", "CONTEXT")
		for _, m := range p.Models {
			ctx := "-"
			if m.ContextSize > 0 {
				ctx = fmt.Sprintf("%d", m.ContextSize)
			}
			fmt.Printf("%-36s %-24s %s\n", m.ID, m.Name, ctx)
		}
		return nil
	},
}

var providerRefreshKeyID string

var providerRefreshModelsCmd = &cobra.Command{
	Use:   "refresh-models <slug>",
	Short: "Refresh a provider model catalog from its models API",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		p := reg.Find(args[0])
		if p == nil {
			return fmt.Errorf("no provider with slug %q", args[0])
		}
		p.Normalize()
		if p.Catalog.Source != "dynamic" && p.ModelPolicy.Source != provider.ModelSourceDynamic && p.Endpoints.ModelsURL == "" {
			return fmt.Errorf("provider %s does not declare a dynamic models endpoint; use its static catalog or edit provider metadata", p.Slug)
		}

		apiKey := ""
		if p.NeedsKey() {
			v, err := loadVault()
			if err != nil {
				return err
			}
			rec := v.Get(providerRefreshKeyID)
			if rec == nil && providerRefreshKeyID == "" {
				for i := range v.Keys {
					if v.Keys[i].ProviderSlug == p.Slug {
						rec = &v.Keys[i]
						break
					}
				}
			}
			if rec == nil {
				return fmt.Errorf("no key found for provider %s; pass --key <id>", p.Slug)
			}
			if rec.ProviderSlug != "" && rec.ProviderSlug != p.Slug {
				return fmt.Errorf("key %s belongs to %s, not %s", rec.ID, rec.ProviderSlug, p.Slug)
			}
			apiKey = rec.Secret
		}

		timeout := time.Duration(loadAppConfig().AdapterVerifyTimeoutSeconds) * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		models, err := provider.RefreshModels(ctx, *p, apiKey)
		if err != nil {
			return err
		}
		p.Models = models
		p.Catalog.Source = "dynamic"
		p.ModelPolicy.Source = provider.ModelSourceDynamic
		if err := reg.Save(config.ProvidersPath(resolvedConfigDir())); err != nil {
			return err
		}
		fmt.Printf("Refreshed %d model(s) for %s\n", len(models), p.Slug)
		return nil
	},
}

// Flags for `provider add`.
var addName, addSlug, addBaseURL, addEnvVar, addAuth, addTags, addNotes string

var providerAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a custom provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		if addSlug == "" {
			return fmt.Errorf("--slug is required")
		}
		if provider.LooksLikeSecret(addName) || provider.LooksLikeSecret(addNotes) {
			return fmt.Errorf("refusing to add provider: name or notes looks like a secret")
		}

		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		p := provider.Provider{
			ID:         addSlug,
			Name:       firstNonEmpty(addName, addSlug),
			Slug:       addSlug,
			BaseURL:    addBaseURL,
			EnvVar:     addEnvVar,
			AuthHeader: addAuth,
		}
		if addTags != "" {
			p.Tags = splitCSV(addTags)
		}
		p.Notes = addNotes
		p.Normalize()
		if err := p.ValidateStrict(); err != nil {
			return redactProviderError(err)
		}
		if err := reg.Add(p); err != nil {
			return redactProviderError(err)
		}
		dir := resolvedConfigDir()
		if err := reg.Save(config.ProvidersPath(dir)); err != nil {
			return fmt.Errorf("save providers: %w", err)
		}
		audit.NewLogger(config.AuditPath(dir)).Log(audit.Event{Event: "provider_added", Provider: addSlug})
		fmt.Printf("Added provider %s\n", addSlug)
		return nil
	},
}

var providerRemoveCmd = &cobra.Command{
	Use:     "remove <slug>",
	Aliases: []string{"rm"},
	Short:   "Remove a provider",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		if err := reg.Remove(args[0]); err != nil {
			return err
		}
		dir := resolvedConfigDir()
		if err := reg.Save(config.ProvidersPath(dir)); err != nil {
			return fmt.Errorf("save providers: %w", err)
		}
		audit.NewLogger(config.AuditPath(dir)).Log(audit.Event{Event: "provider_removed", Provider: args[0]})
		fmt.Printf("Removed provider %s\n", args[0])
		return nil
	},
}

// Flags for `provider edit`.
var editName, editBaseURL, editEnvVar, editAuth, editTags, editNotes string

var providerEditCmd = &cobra.Command{
	Use:   "edit <slug>",
	Short: "Edit a provider interactively",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		p := reg.Find(args[0])
		if p == nil {
			return fmt.Errorf("no provider with slug %q", args[0])
		}
		form, err := interactive.RunProviderEdit(interactive.ProviderForm{
			Slug: p.Slug, Name: p.Name, BaseURL: p.BaseURL, EnvVar: p.EnvVar,
			AuthHeader: p.AuthHeader, Notes: p.Notes,
		})
		if err != nil {
			return err
		}
		p.Name = firstNonEmpty(form.Name, p.Name)
		p.BaseURL = firstNonEmpty(form.BaseURL, p.BaseURL)
		p.EnvVar = firstNonEmpty(form.EnvVar, p.EnvVar)
		p.AuthHeader = firstNonEmpty(form.AuthHeader, p.AuthHeader)
		p.Notes = firstNonEmpty(form.Notes, p.Notes)
		if form.Tags != "" {
			p.Tags = splitCSV(form.Tags)
		}
		p.Normalize()
		if err := p.ValidateStrict(); err != nil {
			return redactProviderError(err)
		}
		dir := resolvedConfigDir()
		if err := reg.Save(config.ProvidersPath(dir)); err != nil {
			return fmt.Errorf("save providers: %w", err)
		}
		audit.NewLogger(config.AuditPath(dir)).Log(audit.Event{Event: "provider_edited", Provider: args[0]})
		fmt.Printf("Updated provider %s\n", args[0])
		return nil
	},
}

var providerSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search providers by name, slug, or tag",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		query := strings.ToLower(args[0])
		var matches []provider.Provider
		for _, p := range reg.Providers {
			if strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.Slug), query) ||
				containsTag(p.Tags, query) {
				matches = append(matches, p)
			}
		}
		if len(matches) == 0 {
			fmt.Printf("No providers matching %q\n", args[0])
			return nil
		}
		fmt.Printf("%-16s %-24s %s\n", "SLUG", "NAME", "ENV VAR")
		for _, p := range matches {
			fmt.Printf("%-16s %-24s %s\n", redactProviderOutput(p.Slug), redactProviderOutput(p.Name), redactProviderOutput(p.EnvVar))
		}
		return nil
	},
}

var providerValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate all provider metadata",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		var problems int
		for _, p := range reg.Providers {
			p.Normalize()
			if err := p.ValidateStrict(); err != nil {
				fmt.Printf("[WARN] %s: %s\n", redactProviderOutput(p.Slug), redactProviderOutput(err.Error()))
				problems++
			}
		}
		if problems == 0 {
			fmt.Println("All providers valid")
			return nil
		}
		return fmt.Errorf("%d problem(s) found", problems)
	},
}

var providerExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export non-secret provider metadata as JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, _, err := loadStores()
		if err != nil {
			return err
		}
		if err := validateProviderRegistryForExport(reg); err != nil {
			return err
		}
		data, err := reg.Serialize()
		if err != nil {
			return fmt.Errorf("serialize: %w", err)
		}
		fmt.Print(redactProviderOutput(string(data)))
		return nil
	},
}

func redactProviderOutput(s string) string {
	return redact.NewRedactor(nil).RedactString(s)
}

func redactProviderError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactProviderOutput(err.Error()))
}

func validateProviderRegistryForExport(reg *provider.Registry) error {
	for i := range reg.Providers {
		p := reg.Providers[i]
		p.Normalize()
		if err := p.ValidateStrict(); err != nil {
			return fmt.Errorf("provider %q failed strict validation; refusing export: %s", p.Slug, redactProviderOutput(err.Error()))
		}
	}
	return nil
}

func init() {
	providerAddCmd.Flags().StringVar(&addSlug, "slug", "", "provider slug (required, unique)")
	providerAddCmd.Flags().StringVar(&addName, "name", "", "display name")
	providerAddCmd.Flags().StringVar(&addBaseURL, "base-url", "", "API base URL (https://...)")
	providerAddCmd.Flags().StringVar(&addEnvVar, "env-var", "", "primary env var name (e.g. OPENAI_API_KEY)")
	providerAddCmd.Flags().StringVar(&addAuth, "auth-header", "", "auth header template (use ${KEY} placeholder)")
	providerAddCmd.Flags().StringVar(&addTags, "tags", "", "comma-separated tags")
	providerAddCmd.Flags().StringVar(&addNotes, "notes", "", "free-form notes")
	providerRefreshModelsCmd.Flags().StringVar(&providerRefreshKeyID, "key", "", "key id to use for authenticated model refresh")

	providerCmd.AddCommand(providerListCmd, providerInspectCmd, providerModelsCmd, providerRefreshModelsCmd, providerAddCmd, providerRemoveCmd, providerEditCmd, providerSearchCmd, providerValidateCmd, providerExportCmd)
	rootCmd.AddCommand(providerCmd)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func containsTag(tags []string, query string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}
