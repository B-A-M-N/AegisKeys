package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/runner"
	"aegiskeys/internal/secret"
)

var adapterVerifyApp string
var adapterVerifyInstalled bool

var adapterCmd = &cobra.Command{
	Use:   "adapter",
	Short: "Verify app adapter behavior",
}

var adapterVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Run isolated adapter verification",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadAppConfig()
		reg := adapter.NewRegistry()
		ids := reg.AllIDs()
		if adapterVerifyApp != "" && adapterVerifyApp != "all" {
			ids = []string{adapterVerifyApp}
		}
		var failed int
		for _, id := range ids {
			result := verifyOneAdapter(id, reg, cfg)
			fmt.Printf("%-12s %-5s %s\n", id, result.status, result.detail)
			if result.status == "FAIL" {
				failed++
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d adapter verification failure(s)", failed)
		}
		return nil
	},
}

type adapterVerifyResult struct {
	status string
	detail string
}

func verifyOneAdapter(id string, reg *adapter.Registry, cfg config.Config) adapterVerifyResult {
	a, ok := reg.Get(id)
	if !ok {
		return adapterVerifyResult{"FAIL", "unknown adapter"}
	}
	if err := adapter.ValidateContract(a.Contract()); err != nil {
		return adapterVerifyResult{"FAIL", "contract: " + err.Error()}
	}
	prov, key := syntheticProviderAndKey(a)
	if prov.Slug == "" {
		return adapterVerifyResult{"SKIP", "no compatible synthetic provider"}
	}
	prof := syntheticProfile(id, prov, a.Contract())
	strategy, err := adapter.ResolveLaunchStrategyForMode(prof, prov, key, reg, adapter.ResolveSave)
	if err != nil {
		if strings.Contains(err.Error(), "launch blocked") || strings.Contains(err.Error(), "no command") {
			return adapterVerifyResult{"SKIP", err.Error()}
		}
		return adapterVerifyResult{"FAIL", "render: " + err.Error()}
	}
	if strategy.Plan.Command == "" || (!strategy.Support.CanLaunch && !strategy.Support.CanLaunchArbitraryCommand) {
		return adapterVerifyResult{"SKIP", "adapter does not launch directly"}
	}

	tmp, err := os.MkdirTemp("", "aegiskeys-adapter-verify-*")
	if err != nil {
		return adapterVerifyResult{"FAIL", err.Error()}
	}
	defer os.RemoveAll(tmp)
	home := filepath.Join(tmp, "home")
	xdg := filepath.Join(tmp, "xdg")
	_ = os.MkdirAll(home, 0700)
	_ = os.MkdirAll(xdg, 0700)
	strategy.Plan.Env["HOME"] = home
	strategy.Plan.Env["XDG_CONFIG_HOME"] = xdg
	strategy.Plan.Env["TMPDIR"] = tmp

	if err := runner.Run(context.Background(), strategy, runner.RunOptions{
		ProfileName: prof.Name,
		ConfigDir:   tmp,
		DryRun:      true,
	}); err != nil {
		if strings.Contains(err.Error(), "launch command is empty") || strings.Contains(err.Error(), "cannot launch directly") {
			return adapterVerifyResult{"SKIP", err.Error()}
		}
		return adapterVerifyResult{"FAIL", "dry-run: " + err.Error()}
	}
	if leak, err := findSyntheticSecret(tmp, key.Secret); err != nil {
		return adapterVerifyResult{"FAIL", err.Error()}
	} else if leak != "" {
		return adapterVerifyResult{"FAIL", "secret leaked to " + leak}
	}

	if adapterVerifyInstalled && strategy.Plan.Command != "" && a.Contract().CanLaunch {
		if _, err := exec.LookPath(strategy.Plan.Command); err != nil {
			return adapterVerifyResult{"SKIP", "not installed"}
		}
		if id == "qwen" {
			if err := prepareQwenSmokeHome(home); err != nil {
				return adapterVerifyResult{"FAIL", "qwen temp home: " + err.Error()}
			}
		}
		// File writes were already materialized and leak-checked above. Do not
		// re-apply them for smoke, otherwise fail-closed TOML/XML protections
		// correctly reject the fresh file as an existing user config.
		strategy.Plan.Files = nil
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.AdapterVerifyTimeoutSeconds)*time.Second)
		defer cancel()
		if err := runner.Run(ctx, strategy, runner.RunOptions{
			ProfileName: prof.Name,
			ConfigDir:   tmp,
			ExtraArgs:   []string{"--help"},
		}); err != nil {
			return adapterVerifyResult{"FAIL", "smoke: " + err.Error()}
		}
		return adapterVerifyResult{"OK", "render/files/no-leak/installed-smoke"}
	}
	return adapterVerifyResult{"OK", "render/files/no-leak"}
}

func syntheticProviderAndKey(a adapter.AppAdapter) (provider.Provider, *secret.SecretRecord) {
	candidates := []provider.Provider{
		provider.DefaultProviders()[0],
		provider.DefaultProviders()[1],
		provider.DefaultProviders()[4],
		provider.DefaultProviders()[6],
	}
	for _, p := range candidates {
		p.Normalize()
		if a.SupportsProvider(p) {
			env := p.CanonicalEnvVar()
			return p, &secret.SecretRecord{
				ID:           "key_verify",
				ProviderSlug: p.Slug,
				Label:        "verify",
				Secret:       "ak_verify_synthetic",
				Kind:         secret.SecretAPIKey,
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
				EnvVarHint:   env,
			}
		}
	}
	return provider.Provider{}, nil
}

func syntheticProfile(appID string, prov provider.Provider, c adapter.AppSupportContract) profile.Profile {
	p := profile.Profile{
		Name:         "verify-" + appID,
		ProviderSlug: prov.Slug,
		KeyID:        "key_verify",
		Target:       profile.TargetConfig{App: appID},
	}
	for _, slot := range c.ModelSlots {
		if slot.Optional {
			continue
		}
		modelID := slot.Default
		if modelID == "" && len(prov.Models) > 0 {
			modelID = prov.Models[0].ID
		}
		if modelID == "" {
			modelID = "verify-model"
		}
		setSyntheticModel(&p.Models, slot.Name, modelID)
	}
	return p
}

func setSyntheticModel(models *profile.ModelSlots, slot, id string) {
	ref := &profile.ModelRef{ID: id, Source: profile.ModelSourceStatic, Locked: true}
	switch slot {
	case "main":
		models.Main = ref
	case "fast":
		models.Fast = ref
	case "weak":
		models.Weak = ref
	case "editor":
		models.Editor = ref
	case "planner":
		models.Planner = ref
	case "actor":
		models.Actor = ref
	case "compression":
		models.Compression = ref
	case "vision":
		models.Vision = ref
	case "web_extract":
		models.WebExtract = ref
	}
}

func prepareQwenSmokeHome(home string) error {
	realQwen, err := exec.LookPath("qwen")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(realQwen)
	if err != nil || !strings.Contains(string(data), "$HOME/.nvm/versions/node/v22.22.1/bin/qwen") {
		return nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	realBinary := filepath.Join(userHome, ".nvm/versions/node/v22.22.1/bin/qwen")
	if _, err := os.Stat(realBinary); err != nil {
		matches, gerr := filepath.Glob(filepath.Join(userHome, ".nvm/versions/node/*/bin/qwen"))
		if gerr != nil {
			return gerr
		}
		if len(matches) == 0 {
			return fmt.Errorf("real qwen binary not found under %s", filepath.Join(userHome, ".nvm/versions/node"))
		}
		realBinary = matches[len(matches)-1]
	}
	link := filepath.Join(home, ".nvm/versions/node/v22.22.1/bin/qwen")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		return err
	}
	if err := os.Symlink(realBinary, link); err != nil && !os.IsExist(err) {
		return err
	}
	keyPath := filepath.Join(home, ".config/openrouter/api_key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return err
	}
	return os.WriteFile(keyPath, []byte("ak_verify_synthetic\n"), 0600)
}

func findSyntheticSecret(root, synthetic string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), synthetic) {
			found = path
		}
		return nil
	})
	return found, err
}

func init() {
	adapterVerifyCmd.Flags().StringVar(&adapterVerifyApp, "app", "all", "adapter id to verify, or all")
	adapterVerifyCmd.Flags().BoolVar(&adapterVerifyInstalled, "installed", false, "also run installed CLI --help smoke tests")
	adapterCmd.AddCommand(adapterVerifyCmd)
	rootCmd.AddCommand(adapterCmd)
}
