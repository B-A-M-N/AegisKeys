package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/runner"
)

// launchCmd is the short-form launcher. It takes a positional profile name
// and optional trailing args that are passed through to the child process.
//
//	aegiskeys launch myprofile
//	aegiskeys l myprofile
//	aegiskeys l myprofile --extra-flag value
//
// The first positional arg is always the profile name; everything after it is
// forwarded as child args, so there is no `--` separator to remember.
var launchCmd = &cobra.Command{
	Use:     "launch <profile> [args...]",
	Aliases: []string{"l", "go", "run"},
	Short:   "Launch a profile's target app with secrets injected",
	Long: "Launches the app configured for a profile with secrets injected.\n" +
		"The first argument is the profile name; any extra args are forwarded to the child.\n\n" +
		"  ak launch myprofile          # launch the profile's app\n" +
		"  ak l myprofile               # same (short form)\n" +
		"  ak l myprofile --verbose     # launch with an extra arg\n" +
		"  ak go myprofile sh -c '…'    # launch with a full command override",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]
		extraArgs := args[1:]

		pw, err := promptPassword()
		if err != nil {
			return err
		}
		reg, store, err := loadStores()
		if err != nil {
			return err
		}
		prof := store.Find(profileName)
		if prof == nil {
			return fmt.Errorf("no profile named %q", profileName)
		}
		prov := reg.Find(prof.ProviderSlug)
		if prov == nil {
			return fmt.Errorf("profile %q references missing provider %q", prof.Name, prof.ProviderSlug)
		}
		v, err := openVault(pw)
		if err != nil {
			return err
		}
		rec := v.Get(prof.KeyID)
		if rec == nil {
			return fmt.Errorf("profile %q references missing key %q", prof.Name, prof.KeyID)
		}

		adapterReg := adapter.NewRegistry()
		strategy, err := adapter.ResolveLaunchStrategyCatalog(*prof, *prov, rec, adapterReg, reg, v, adapter.ResolveRun)
		if err != nil {
			return err
		}

		if len(strategy.Hazards) > 0 {
			fmt.Println("Warnings:")
			for _, h := range strategy.Hazards {
				fmt.Printf("  [%s] %s\n", h.Severity, h.Title)
				if h.Fix != "" {
					fmt.Printf("    fix: %s\n", h.Fix)
				}
			}
		}

		v.Touch(prof.KeyID)
		if err := saveVault(pw, v); err != nil {
			return err
		}

		// If the user supplied a bare profile name with no trailing args, use
		// the strategy-resolved command. If trailing args are present and the
		// adapter already resolved a command, forward the trailing args as
		// ExtraArgs. If no command was resolved (e.g. generic profile), treat
		// the first trailing arg as the command and the rest as ExtraArgs.
		finalExtraArgs := extraArgs
		if strategy.Plan.Command == "" && len(extraArgs) > 0 {
			strategy.Plan.Command = extraArgs[0]
			finalExtraArgs = extraArgs[1:]
		}

		return runner.Run(context.Background(), strategy, runner.RunOptions{
			ProfileName:  prof.Name,
			ConfigDir:    resolvedConfigDir(),
			ExtraArgs:    finalExtraArgs,
			InheritStdio: true,
		})
	},
}

func init() {
	rootCmd.AddCommand(launchCmd)
}
