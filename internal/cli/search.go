package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search apps, groups, and members together",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			results := c.Search(args[0])
			if flagJSON {
				return writeJSON(results)
			}
			if len(results) == 0 {
				fmt.Fprintf(os.Stdout, "no app, group, or member matches %q\n", args[0])
				return nil
			}
			t := newTable(os.Stdout)
			t.row("TYPE", "NAME", "DETAIL", "ID")
			for _, result := range results {
				t.row(result.Kind, result.Label, result.Detail, result.ID)
			}
			t.flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output JSON")
	return cmd
}
