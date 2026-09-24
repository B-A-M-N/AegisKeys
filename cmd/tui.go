package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/tui"
)

var tuiCmd = &cobra.Command{Use: "tui", Short: "Launch the interactive terminal UI", RunE: func(cmd *cobra.Command, args []string) error { return runTUI(cmd) }}
var recoverConfig bool
var recoverConfigCmd = &cobra.Command{
	Use: "recover-config --confirm", Short: "Back up and explicitly reset malformed provider/profile/settings files", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !recoverConfig {
			return fmt.Errorf("--confirm is required; damaged files are preserved otherwise")
		}
		return recoverMalformedConfig(resolvedConfigDir())
	},
}

type recoveryFile struct{ path, name, fresh string }

func recoverMalformedConfig(dir string) error {
	providersPath, profilesPath, configPath := config.ProvidersPath(dir), config.ProfilesPath(dir), config.ConfigPath(dir)
	reg, regErr := provider.LoadRegistry(providersPath)
	store, storeErr := profile.LoadStore(profilesPath)
	cfg, cfgErr := config.LoadConfig(configPath)
	files := []recoveryFile{}
	if regErr != nil && !errors.Is(regErr, os.ErrNotExist) {
		r := provider.NewRegistry()
		r.MergeDefaults(provider.DefaultProviders())
		b, _ := r.Serialize()
		files = append(files, recoveryFile{providersPath, "providers", string(b)})
	}
	if storeErr != nil && !errors.Is(storeErr, os.ErrNotExist) {
		files = append(files, recoveryFile{profilesPath, "profiles", mustJSON(profile.NewStore())})
	}
	if cfgErr != nil && !errors.Is(cfgErr, os.ErrNotExist) {
		files = append(files, recoveryFile{configPath, "settings", mustJSON(config.DefaultConfig())})
	}
	_ = reg
	_ = store
	_ = cfg
	if len(files) == 0 {
		return fmt.Errorf("no malformed provider/profile/settings files were found; nothing changed")
	}
	fmt.Println("Malformed files that will be backed up and reset:")
	for _, f := range files {
		fmt.Println("  " + f.name + ": " + f.path)
	}
	fmt.Print("Type RESET to continue: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "RESET" {
		return fmt.Errorf("aborted")
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backedUp := make([]recoveryFile, 0, len(files))
	for _, f := range files {
		backup := f.path + ".damaged." + stamp
		if err := os.Rename(f.path, backup); err != nil {
			rollbackRecovery(backedUp)
			return fmt.Errorf("backup %s: %w", f.path, err)
		}
		backedUp = append(backedUp, recoveryFile{path: backup, name: f.name, fresh: f.fresh})
	}
	for i, f := range backedUp {
		if err := os.WriteFile(files[i].path, []byte(f.fresh), 0600); err != nil {
			rollbackRecovery(backedUp)
			return err
		}
	}
	return nil
}

func mustJSON(v any) string             { data, _ := jsonMarshal(v); return string(data) }
func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", " ") }

func rollbackRecovery(backups []recoveryFile) {
	for _, b := range backups {
		_ = os.Rename(b.path, originalRecoveryPath(b.path))
	}
}
func originalRecoveryPath(backup string) string {
	i := strings.Index(backup, ".damaged.")
	if i < 0 {
		return backup
	}
	return backup[:i]
}

func init() {
	recoverConfigCmd.Flags().BoolVar(&recoverConfig, "confirm", false, "required explicit confirmation flag")
	rootCmd.AddCommand(tuiCmd, recoverConfigCmd)
}
func runTUI(_ *cobra.Command) error {
	if err := requireInitialized(); err != nil {
		return err
	}
	if err := tui.Run(resolvedConfigDir(), version); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
