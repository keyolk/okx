package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/okta"
)

var (
	flagDryRun bool
	flagYes    bool
	flagGroup  bool
)

// change is one pending assignment mutation.
type change struct {
	verb    string // "assign" or "unassign"
	kind    string // "user" or "group"
	appID   string
	appName string
	id      string
	label   string
	// noop is set when the target state already holds; the change is reported
	// and skipped rather than sent to Okta.
	noop string
	// warn carries the "this will not actually revoke access" caveat.
	warn string
}

func (ch change) line() string {
	arrow := "+"
	if ch.verb == "unassign" {
		arrow = "-"
	}
	s := fmt.Sprintf("  %s %s %s  →  %s", arrow, ch.kind, ch.label, ch.appName)
	if ch.noop != "" {
		s += "  (skip: " + ch.noop + ")"
	}
	return s
}

func newAssignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <app> <user|group>...",
		Short: "Assign users or groups to an app",
		Long: `Assign one or more users (default) or groups (--group) to an app.

Targets are resolved by ID, exact login/name, or unique fuzzy match. Every
change is previewed and confirmed before anything is sent to Okta; use
--dry-run to preview only, or --yes to skip the prompt in scripts.`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeAssignArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMutation(cmd.Context(), "assign", args[0], args[1:])
		},
	}
	addMutationFlags(cmd)
	return cmd
}

func newUnassignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "unassign <app> <user|group>...",
		Short:   "Remove users or groups from an app",
		Aliases: []string{"remove", "rm"},
		Long: `Remove one or more direct user assignments (default) or group
assignments (--group) from an app.

Removing a direct user assignment does not revoke access the user still holds
through an assigned group — okx flags that case explicitly in the preview.`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeUnassignArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMutation(cmd.Context(), "unassign", args[0], args[1:])
		},
	}
	addMutationFlags(cmd)
	return cmd
}

func addMutationFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.BoolVar(&flagGroup, "group", false, "targets are groups instead of users")
	f.BoolVarP(&flagDryRun, "dry-run", "n", false, "preview the changes without applying them")
	f.BoolVarP(&flagYes, "yes", "y", false, "apply without the confirmation prompt")
	f.BoolVar(&flagJSON, "json", false, "output the result as JSON")
}

func runMutation(ctx context.Context, verb, appQuery string, targets []string) error {
	c, err := open(ctx)
	if err != nil {
		return err
	}
	app, err := c.ResolveApp(appQuery)
	if err != nil {
		return err
	}

	changes, err := planChanges(c, verb, app, targets)
	if err != nil {
		return err
	}

	// Preview.
	fmt.Fprintf(os.Stderr, "%s on %s (%s)\n", verb, app.Label, app.ID)
	pending := 0
	for _, ch := range changes {
		fmt.Fprintln(os.Stderr, ch.line())
		if ch.warn != "" {
			fmt.Fprintf(os.Stderr, "      ! %s\n", ch.warn)
		}
		if ch.noop == "" {
			pending++
		}
	}
	if pending == 0 {
		fmt.Fprintln(os.Stderr, "\nnothing to do")
		return nil
	}

	if flagDryRun {
		fmt.Fprintf(os.Stderr, "\ndry run — %d change(s) not applied\n", pending)
		return nil
	}
	if !flagYes {
		ok, err := confirm(fmt.Sprintf("\napply %d change(s)?", pending))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted")
		}
	}

	type result struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
		App    string `json:"app"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var results []result
	var failed int

	for _, ch := range changes {
		if ch.noop != "" {
			results = append(results, result{ch.kind, ch.label, ch.appName, "skipped", ch.noop})
			continue
		}
		err := applyChange(ctx, c, ch)
		switch {
		case err == nil:
			results = append(results, result{ch.kind, ch.label, ch.appName, "ok", ""})
			if !flagJSON {
				fmt.Fprintf(os.Stderr, "  ✓ %s %s\n", ch.verb, ch.label)
			}
		case verb == "unassign" && okta.NotFound(err):
			// Already gone: the desired end state holds.
			results = append(results, result{ch.kind, ch.label, ch.appName, "skipped", "not assigned"})
		default:
			failed++
			results = append(results, result{ch.kind, ch.label, ch.appName, "error", err.Error()})
			if !flagJSON {
				fmt.Fprintf(os.Stderr, "  ✗ %s %s: %v\n", ch.verb, ch.label, err)
			}
		}
	}

	// The snapshot is stale the moment a write lands. Cache deletion failure must
	// remain visible, but it must not report already-applied Okta writes as failed.
	if err := c.Invalidate(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not invalidate cache: %v\n", err)
	}

	if flagJSON {
		if err := writeJSON(results); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d change(s) failed", failed, pending)
	}
	return nil
}

func planChanges(c *okxapp.Context, verb string, app okta.App, targets []string) ([]change, error) {
	var changes []change
	for _, q := range targets {
		if flagGroup {
			g, err := c.ResolveGroup(q)
			if err != nil {
				return nil, err
			}
			ch := change{verb: verb, kind: "group", appID: app.ID, appName: app.Label,
				id: g.ID, label: g.Profile.Name}
			assigned := c.AppHasGroup(app.ID, g.ID)
			switch {
			case verb == "assign" && assigned:
				ch.noop = "already assigned"
			case verb == "unassign" && !assigned:
				ch.noop = "not assigned"
			case verb == "unassign":
				n := len(c.Index.GroupMembers[g.ID])
				ch.warn = fmt.Sprintf("removes app access for %d group member(s)", n)
			}
			changes = append(changes, ch)
			continue
		}

		u, err := c.ResolveUser(q)
		if err != nil {
			return nil, err
		}
		ch := change{verb: verb, kind: "user", appID: app.ID, appName: app.Label,
			id: u.ID, label: u.Profile.Login}
		found, direct, viaGroups := c.AppUserScope(app.ID, u.ID)
		switch {
		case verb == "assign" && direct:
			ch.noop = "already assigned directly"
		case verb == "assign" && found:
			ch.warn = "already has access via " + groupList(viaGroups) + "; adding a direct assignment too"
		case verb == "unassign" && !found:
			ch.noop = "not assigned"
		case verb == "unassign" && !direct:
			ch.noop = "only has access via " + groupList(viaGroups) + " — remove the group assignment instead"
		case verb == "unassign" && len(viaGroups) > 0:
			ch.warn = "still keeps access via " + groupList(viaGroups)
		}
		changes = append(changes, ch)
	}
	return changes, nil
}

func applyChange(ctx context.Context, c *okxapp.Context, ch change) error {
	switch {
	case ch.kind == "group" && ch.verb == "assign":
		_, err := c.Client.AssignGroup(ctx, ch.appID, ch.id)
		return err
	case ch.kind == "group":
		return c.Client.UnassignGroup(ctx, ch.appID, ch.id)
	case ch.verb == "assign":
		_, err := c.Client.AssignUser(ctx, ch.appID, ch.id)
		return err
	default:
		return c.Client.UnassignUser(ctx, ch.appID, ch.id)
	}
}

func groupList(gs []okta.Group) string {
	if len(gs) == 0 {
		return "a group"
	}
	var ns []string
	for _, g := range gs {
		ns = append(ns, g.Profile.Name)
	}
	return strings.Join(ns, ", ")
}
