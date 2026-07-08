package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/resolve"
)

var profileCmd = &cobra.Command{
	Use:     "profile",
	Aliases: []string{"prof"},
	Short:   "Manage profiles that bind providers to keys",
}

// Flags for `profile create`.
var (
	profCreateName        string
	profCreateProvider    string
	profCreateKey         string
	profCreateAlias       string
	profCreateEnv         string
	profCreateNotes       string
	profCreateApp         string
	profCreateModelMain   string
	profCreateModelWeak   string
	profCreateModelEditor string
)

var profileCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a profile binding a provider to a key",
	Long: "Creates a profile that binds a provider + key + model config to a target app.\n" +
		"Use --app to select the target application (determines injection strategy).\n" +
		"Use --model-main, --model-weak, etc. to assign per-app model roles.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireInitialized(); err != nil {
			return err
		}
		if profCreateName == "" {
			return fmt.Errorf("--name is required")
		}
		if profCreateProvider == "" {
			return fmt.Errorf("--provider is required")
		}
		if profCreateKey == "" {
			return fmt.Errorf("--key is required")
		}
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		prov := reg.Find(profCreateProvider)
		if prov == nil {
			return fmt.Errorf("unknown provider %q", profCreateProvider)
		}
		prov.Normalize()
		if err := prov.ValidateStrict(); err != nil {
			return fmt.Errorf("provider %q is invalid: %w", profCreateProvider, err)
		}
		// Verify the key exists in the vault. Requires unlock.
		v, err := loadVault()
		if err != nil {
			return err
		}
		rec := v.Get(profCreateKey)
		if rec == nil {
			return fmt.Errorf("no key with id %q", profCreateKey)
		}
		if rec.ProviderSlug != "" && rec.ProviderSlug != prov.Slug {
			return fmt.Errorf("key %q belongs to provider %q, not %q", profCreateKey, rec.ProviderSlug, prov.Slug)
		}

		appID := profCreateApp
		if appID == "" {
			appID = "generic"
		}

		p := profile.Profile{
			Name:         profCreateName,
			ProviderSlug: profCreateProvider,
			KeyID:        profCreateKey,
			Notes:        profCreateNotes,
			Target: profile.TargetConfig{
				App: appID,
			},
		}
		if profCreateAlias != "" {
			p.Aliases = splitCSV(profCreateAlias)
		}
		if profCreateEnv != "" {
			env, err := parseEnvFlag(profCreateEnv)
			if err != nil {
				return err
			}
			p.Env = env
		}

		// Apply model slot assignments from flags. Interactive setup should
		// choose from provider catalogs; CLI flags remain explicit.
		if profCreateModelMain != "" {
			if p.Models.Main == nil {
				p.Models.Main = &profile.ModelRef{ID: profCreateModelMain, Source: profile.ModelSourceManual}
			} else {
				p.Models.Main.ID = profCreateModelMain
			}
		}
		if profCreateModelWeak != "" {
			p.Models.Weak = &profile.ModelRef{ID: profCreateModelWeak, Source: profile.ModelSourceManual}
		}
		if profCreateModelEditor != "" {
			p.Models.Editor = &profile.ModelRef{ID: profCreateModelEditor, Source: profile.ModelSourceManual}
		}

		adapterReg := adapter.NewRegistry()
		a, ok := adapterReg.Get(appID)
		if !ok {
			return fmt.Errorf("unknown target app %q (available: %s)", appID, strings.Join(adapterReg.AllIDs(), ", "))
		}
		p.Target.RenderMode = renderModeForContract(a.Contract())
		warnings, err := a.Validate(p, *prov)
		if err != nil {
			return fmt.Errorf("validation: %w", err)
		}
		for _, w := range warnings {
			fmt.Printf("  warning: %s\n", w)
		}
		if err := resolve.ValidateResolution(p, reg, v, adapterReg); err != nil {
			return err
		}

		if strategy, err := adapter.ResolveLaunchStrategyCatalog(p, *prov, rec, adapterReg, reg, v, adapter.ResolveSave); err == nil {
			if len(strategy.Hazards) > 0 {
				fmt.Println("Warnings:")
				for _, h := range strategy.Hazards {
					fmt.Printf("  [%s] %s\n", h.Severity, h.Title)
					if h.Fix != "" {
						fmt.Printf("    fix: %s\n", h.Fix)
					}
				}
			}
		}

		if err := store.Add(p); err != nil {
			return err
		}
		if err := profile.SaveStore(config.ProfilesPath(resolvedConfigDir()), store); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{
			Event:    "profile_created",
			Profile:  profCreateName,
			Provider: profCreateProvider,
		})
		fmt.Printf("Created profile %s\n", profCreateName)
		return nil
	},
}

var profileListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		if len(store.Profiles) == 0 {
			fmt.Println("No profiles configured. Run `aegiskeys profile create`.")
			return nil
		}
		fmt.Printf("%-18s %-16s %-18s %s\n", "NAME", "PROVIDER", "KEY", "ALIASES")
		for _, p := range store.Profiles {
			provName := p.ProviderSlug
			if pr := reg.Find(p.ProviderSlug); pr != nil {
				provName = pr.Name
			}
			fmt.Printf("%-18s %-16s %-18s %s\n", p.Name, provName, p.KeyID, joinStr(p.Aliases, ", "))
		}
		return nil
	},
}

var profileInspectCmd = &cobra.Command{
	Use:   "inspect <name>",
	Short: "Show details for a profile including adapter contract info",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		p := store.Find(args[0])
		if p == nil {
			return fmt.Errorf("no profile named %q", args[0])
		}
		provName := p.ProviderSlug
		if pr := reg.Find(p.ProviderSlug); pr != nil {
			provName = pr.Name
		}
		fmt.Printf("Name:      %s\n", p.Name)
		fmt.Printf("Provider:  %s (%s)\n", provName, p.ProviderSlug)
		fmt.Printf("Key:       %s\n", p.KeyID)
		if len(p.Aliases) > 0 {
			fmt.Printf("Aliases:   %s\n", joinStr(p.Aliases, ", "))
		}

		// Show adapter info.
		appID := p.TargetApp()
		fmt.Printf("Target:    %s\n", appID)
		adapterReg := adapter.NewRegistry()
		if a, ok := adapterReg.Get(appID); ok {
			c := a.Contract()
			fmt.Printf("Support:   %s\n", c.SupportLevel)
			fmt.Printf("App class: %v\n", c.LaunchSurfaces)
			if len(c.ModelSlots) > 0 {
				slots := make([]string, 0, len(c.ModelSlots))
				for _, s := range c.ModelSlots {
					slots = append(slots, s.Name)
				}
				fmt.Printf("Slots:     %s\n", strings.Join(slots, ", "))
			}
		}

		// Show assigned models.
		if p.Models.Main != nil {
			fmt.Printf("Models:\n")
			printModel := func(name string, m *profile.ModelRef) {
				if m != nil {
					fmt.Printf("  %s: %s\n", name, m.ID)
				}
			}
			printModel("main", p.Models.Main)
			printModel("fast", p.Models.Fast)
			printModel("weak", p.Models.Weak)
			printModel("editor", p.Models.Editor)
			printModel("planner", p.Models.Planner)
			printModel("actor", p.Models.Actor)
			printModel("compression", p.Models.Compression)
			printModel("vision", p.Models.Vision)
			printModel("web_extract", p.Models.WebExtract)
		}

		if len(p.Env) > 0 {
			fmt.Println("Env overrides:")
			for k, vv := range p.Env {
				fmt.Printf("  %s=%s\n", k, vv)
			}
		}
		if p.Notes != "" {
			fmt.Printf("Notes:     %s\n", p.Notes)
		}
		return nil
	},
}

var profileDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a profile",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := loadStores()
		if err != nil {
			return err
		}
		if store.Find(args[0]) == nil {
			return fmt.Errorf("no profile named %q", args[0])
		}
		if err := store.Remove(args[0]); err != nil {
			return err
		}
		if err := profile.SaveStore(config.ProfilesPath(resolvedConfigDir()), store); err != nil {
			return err
		}
		audit.NewLogger(config.AuditPath(resolvedConfigDir())).Log(audit.Event{Event: "profile_deleted", Profile: args[0]})
		fmt.Printf("Deleted profile %s\n", args[0])
		return nil
	},
}

func init() {
	profileCreateCmd.Flags().StringVar(&profCreateName, "name", "", "profile name (required, unique)")
	profileCreateCmd.Flags().StringVar(&profCreateProvider, "provider", "", "provider slug (required)")
	profileCreateCmd.Flags().StringVar(&profCreateKey, "key", "", "key id (required)")
	profileCreateCmd.Flags().StringVar(&profCreateAlias, "alias", "", "comma-separated aliases")
	profileCreateCmd.Flags().StringVar(&profCreateEnv, "env", "", "extra env: KEY=VAL,KEY2=VAL2")
	profileCreateCmd.Flags().StringVar(&profCreateNotes, "notes", "", "free-form notes")
	profileCreateCmd.Flags().StringVar(&profCreateApp, "app", "", "target app (e.g. aider, crush, zed, intellij; defaults to generic)")
	profileCreateCmd.Flags().StringVar(&profCreateModelMain, "model-main", "", "model ID for main role")
	profileCreateCmd.Flags().StringVar(&profCreateModelWeak, "model-weak", "", "model ID for weak role (Aider)")
	profileCreateCmd.Flags().StringVar(&profCreateModelEditor, "model-editor", "", "model ID for editor role (Aider)")

	profileCmd.AddCommand(profileCreateCmd, profileListCmd, profileInspectCmd, profileDeleteCmd)
	rootCmd.AddCommand(profileCmd)
}

func renderModeForContract(c adapter.AppSupportContract) profile.RenderMode {
	switch {
	case c.CanPatchConfig && c.CanInjectSecrets:
		return profile.RenderEnvConfig
	case c.CanPatchConfig:
		return profile.RenderConfigFile
	case c.CanInjectSecrets:
		return profile.RenderEnv
	default:
		return profile.RenderEnv
	}
}

// parseEnvFlag parses a "K=V,K2=V2" string into a map.
func parseEnvFlag(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range splitCSV(s) {
		idx := indexByte(pair, '=')
		if idx < 0 {
			return nil, fmt.Errorf("env pair %q missing '='", pair)
		}
		out[pair[:idx]] = pair[idx+1:]
	}
	return out, nil
}

// indexByte returns the index of b in s, or -1.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func joinStr(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
