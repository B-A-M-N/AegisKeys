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
	"aegiskeys/internal/fsutil"
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
	for _, f := range files {
		backup := f.path + ".damaged." + stamp
		original, err := os.ReadFile(f.path)
		if err != nil {
			return fmt.Errorf("read damaged file %s: %w", f.path, err)
		}
		if err := fsutil.AtomicWriteFile(backup, original); err != nil {
			return fmt.Errorf("backup %s: %w", f.path, err)
		}
	}
	for _, f := range files {
		if recoveryCrashPoint != nil {
			recoveryCrashPoint("before-replace:" + f.name)
		}
		if err := fsutil.AtomicWriteFile(f.path, []byte(f.fresh)); err != nil {
			return err
		}
	}
	return nil
}

var recoveryCrashPoint func(string)

func mustJSON(v any) string             { data, _ := jsonMarshal(v); return string(data) }
func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", " ") }

func rollbackRecovery(backups []recoveryFile) {
	for _, b := range backups {
		original := originalRecoveryPath(b.path)
		_ = os.Remove(original)
		_ = os.Rename(b.path, original)
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
	if err := offerConfigRecovery(resolvedConfigDir()); err != nil {
		return err
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	if err := tui.Run(resolvedConfigDir(), version); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func offerConfigRecovery(dir string) error {
	_, regErr := provider.LoadRegistry(config.ProvidersPath(dir))
	_, storeErr := profile.LoadStore(config.ProfilesPath(dir))
	_, cfgErr := config.LoadConfig(config.ConfigPath(dir))
	damaged := (regErr != nil && !errors.Is(regErr, os.ErrNotExist)) || (storeErr != nil && !errors.Is(storeErr, os.ErrNotExist)) || (cfgErr != nil && !errors.Is(cfgErr, os.ErrNotExist))
	if !damaged {
		return nil
	}
	fmt.Println("AegisKeys found malformed provider/profile/settings metadata. Files remain untouched unless you explicitly recover them.")
	return recoverMalformedConfig(dir)
}
