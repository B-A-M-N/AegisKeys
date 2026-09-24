package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegiskeys/internal/config"
	"aegiskeys/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(cmd)
	},
}

var recoverConfig bool

var recoverConfigCmd = &cobra.Command{
	Use:   "recover-config --confirm",
	Short: "Back up and explicitly reset malformed provider/profile/settings files",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !recoverConfig {
			return fmt.Errorf("--confirm is required; damaged files are preserved otherwise")
		}
		return recoverMalformedConfig(resolvedConfigDir())
	},
}

func recoverMalformedConfig(dir string) error {
	paths := []string{config.ProvidersPath(dir), config.ProfilesPath(dir), config.ConfigPath(dir)}
	damaged := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			damaged = append(damaged, path)
		} else {
			return fmt.Errorf("cannot recover missing file %s", path)
		}
	}
	fmt.Println("The following existing files will be backed up and reset:")
	for _, path := range damaged {
		fmt.Println("  " + path)
	}
	fmt.Print("Type RESET to continue: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "RESET" {
		return fmt.Errorf("aborted")
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for _, path := range damaged {
		backup := path + ".damaged." + stamp
		if err := os.Rename(path, backup); err != nil {
			return fmt.Errorf("backup %s: %w", path, err)
		}
	}
	if err := os.WriteFile(config.ProvidersPath(dir), []byte("{}\n"), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(config.ProfilesPath(dir), []byte("{}\n"), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(config.ConfigPath(dir), []byte("{}\n"), 0600); err != nil {
		return err
	}
	return nil
}

func init() {
	recoverConfigCmd.Flags().BoolVar(&recoverConfig, "confirm", false, "required explicit confirmation flag")
	rootCmd.AddCommand(tuiCmd, recoverConfigCmd)
}

// runTUI launches the interactive TUI. It is also the default action when
// aegiskeys is invoked with no subcommand (see root.go).
func runTUI(_ *cobra.Command) error {
	if err := requireInitialized(); err != nil {
		return err
	}
	// The TUI loads provider/profile/vault state itself; see internal/tui.
	if err := tui.Run(resolvedConfigDir(), version); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
