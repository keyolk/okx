// Package cli implements okx's command-line interface.
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	okxapp "github.com/keyolk/okx/internal/app"
)

// Global flags shared by every command.
var (
	flagConfig  string
	flagRefresh bool
	flagTTL     time.Duration
	flagJSON    bool
	flagQuiet   bool
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Execute runs the root command.
func Execute() error {
	return ExecuteContext(context.Background())
}

// ExecuteContext runs the root command with a cancellable context, so that
// SIGINT unwinds an in-flight fetch instead of killing the process mid-write.
func ExecuteContext(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "okx",
		Short: "Okta app assignment manager",
		Long: `okx manages Okta application assignments for users and groups.

Reads its org URL and API token from ~/.okta/okta.yaml (the same file the
official okta CLI uses), or from $OKTA_ORG_URL and $OKTA_API_TOKEN.

Running okx with no subcommand opens the interactive TUI.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context())
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flagConfig, "config", "", "path to okta.yaml (default ~/.okta/okta.yaml)")
	pf.BoolVar(&flagRefresh, "refresh", false, "force a cache refresh before running")
	pf.DurationVar(&flagTTL, "ttl", okxapp.DefaultTTL, "how long a cached snapshot stays fresh")
	pf.BoolVar(&flagQuiet, "quiet", false, "suppress progress output")

	root.AddCommand(
		newAppsCmd(),
		newShowCmd(),
		newWhoamiCmd(),
		newUserCmd(),
		newGroupCmd(),
		newAssignCmd(),
		newUnassignCmd(),
		newRefreshCmd(),
		newTUICmd(),
		newCompletionCmd(root),
	)
	return root
}

// open builds the shared app context using the global flags.
func open(ctx context.Context) (*okxapp.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return okxapp.Open(ctx, okxapp.Options{
		ConfigPath: flagConfig,
		Refresh:    flagRefresh,
		TTL:        flagTTL,
		Quiet:      flagQuiet || flagJSON,
	})
}

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refetch the org snapshot into the local cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flagRefresh = true
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "cache: %s\n", c.Path)
			return nil
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the identity and reach of the configured API token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			me, err := c.Client.Me(cmd.Context())
			if err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(map[string]any{
					"org":    c.Cfg.OrgURL,
					"login":  me.Profile.Login,
					"id":     me.ID,
					"apps":   len(c.Index.Apps),
					"users":  len(c.Index.Users),
					"groups": len(c.Index.Groups),
				})
			}
			fmt.Printf("org      %s\n", c.Cfg.OrgURL)
			fmt.Printf("token as %s (%s)\n", me.Profile.Login, me.ID)
			fmt.Printf("visible  %d apps, %d users, %d groups\n",
				len(c.Index.Apps), len(c.Index.Users), len(c.Index.Groups))
			fmt.Printf("cache    %s (age %s)\n", c.Path, c.Index.Age().Round(time.Second))
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output JSON")
	return cmd
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive assignment browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Context())
		},
	}
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish]",
		Short:     "Generate a shell completion script",
		Args:      cobra.ExactValidArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			}
			return fmt.Errorf("unsupported shell %q", args[0])
		},
	}
}
