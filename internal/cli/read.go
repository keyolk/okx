package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
)

func newAppsCmd() *cobra.Command {
	var showCounts bool
	cmd := &cobra.Command{
		Use:     "apps [filter]",
		Short:   "List applications visible to the token",
		Aliases: []string{"ls"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			apps := c.Index.Apps
			if len(args) > 0 {
				filtered := apps[:0:0]
				for _, a := range apps {
					if matchesAny(args[0], a.Label, a.Name, a.ID) {
						filtered = append(filtered, a)
					}
				}
				apps = filtered
			}
			sort.Slice(apps, func(i, j int) bool { return apps[i].Label < apps[j].Label })

			if flagJSON {
				type row struct {
					ID         string `json:"id"`
					Label      string `json:"label"`
					Name       string `json:"name"`
					Status     string `json:"status"`
					SignOnMode string `json:"signOnMode"`
					Users      int    `json:"users"`
					Groups     int    `json:"groups"`
					Direct     int    `json:"directUsers"`
				}
				rows := make([]row, 0, len(apps))
				for _, a := range apps {
					direct := 0
					for _, au := range c.Index.AppUsers[a.ID] {
						if au.Scope == "USER" {
							direct++
						}
					}
					rows = append(rows, row{a.ID, a.Label, a.Name, a.Status, a.SignOnMode,
						len(c.Index.AppUsers[a.ID]), len(c.Index.AppGroups[a.ID]), direct})
				}
				return writeJSON(rows)
			}

			t := newTable(os.Stdout)
			if showCounts {
				t.row("LABEL", "STATUS", "SIGN-ON", "USERS", "DIRECT", "GROUPS", "ID")
			} else {
				t.row("LABEL", "STATUS", "SIGN-ON", "USERS", "GROUPS", "ID")
			}
			for _, a := range apps {
				direct := 0
				for _, au := range c.Index.AppUsers[a.ID] {
					if au.Scope == "USER" {
						direct++
					}
				}
				cells := []string{a.Label, a.Status, a.SignOnMode,
					strconv.Itoa(len(c.Index.AppUsers[a.ID]))}
				if showCounts {
					cells = append(cells, strconv.Itoa(direct))
				}
				cells = append(cells, strconv.Itoa(len(c.Index.AppGroups[a.ID])), a.ID)
				t.row(cells...)
			}
			t.flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output JSON")
	cmd.Flags().BoolVar(&showCounts, "counts", false, "break out the direct-assignment count")
	return cmd
}

func newShowCmd() *cobra.Command {
	var (
		onlyDirect bool
		onlyGroup  bool
		asCSV      bool
		groupsOnly bool
	)
	cmd := &cobra.Command{
		Use:   "show <app>",
		Short: "Show who is assigned to an app, and why",
		Long: `Show every user with access to an app, attributing each one to a direct
assignment, the groups that grant it, or both.

A user shown as "direct + N group(s)" keeps access after an unassign — the
group assignment is what actually needs to change.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			app, err := c.ResolveApp(args[0])
			if err != nil {
				return err
			}

			if groupsOnly {
				return showAppGroups(c, app.ID, app.Label, asCSV)
			}

			assignments := c.Assignments(app.ID)
			filtered := assignments[:0:0]
			for _, a := range assignments {
				switch {
				case onlyDirect && !a.Direct:
					continue
				case onlyGroup && a.Direct:
					continue
				}
				filtered = append(filtered, a)
			}

			if flagJSON {
				type row struct {
					ID     string   `json:"id"`
					Login  string   `json:"login"`
					Name   string   `json:"name"`
					Status string   `json:"status"`
					Direct bool     `json:"direct"`
					Groups []string `json:"viaGroups"`
				}
				rows := make([]row, 0, len(filtered))
				for _, a := range filtered {
					var gs []string
					for _, g := range a.ViaGroups {
						gs = append(gs, g.Profile.Name)
					}
					rows = append(rows, row{a.User.ID, a.User.Profile.Login, a.User.Name(),
						a.Status, a.Direct, gs})
				}
				return writeJSON(map[string]any{
					"app":         map[string]string{"id": app.ID, "label": app.Label},
					"assignments": rows,
				})
			}

			if asCSV {
				rows := make([][]string, 0, len(filtered))
				for _, a := range filtered {
					rows = append(rows, []string{
						a.User.Profile.Login, a.User.Name(), a.Status,
						boolYN(a.Direct), groupNames(a),
					})
				}
				return writeCSV([]string{"login", "name", "status", "direct", "via_groups"}, rows)
			}

			directN, groupN := 0, 0
			for _, a := range assignments {
				if a.Direct {
					directN++
				}
				if len(a.ViaGroups) > 0 || !a.Direct {
					groupN++
				}
			}
			fmt.Printf("%s  (%s)\n", app.Label, app.ID)
			fmt.Printf("%d users — %d direct, %d via %d assigned group(s)\n\n",
				len(assignments), directN, groupN, len(c.Index.AppGroups[app.ID]))

			t := newTable(os.Stdout)
			t.row("LOGIN", "NAME", "STATUS", "ACCESS VIA")
			for _, a := range filtered {
				t.row(a.User.Profile.Login, a.User.Name(), a.Status, accessVia(a))
			}
			t.flush()
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&flagJSON, "json", false, "output JSON")
	f.BoolVar(&asCSV, "csv", false, "output CSV")
	f.BoolVar(&onlyDirect, "direct", false, "only users assigned directly")
	f.BoolVar(&onlyGroup, "group-only", false, "only users who have the app solely via groups")
	f.BoolVar(&groupsOnly, "groups", false, "list the assigned groups instead of users")
	return cmd
}

func showAppGroups(c *okxapp.Context, appID, label string, asCSV bool) error {
	ags := c.Index.AppGroups[appID]
	type row struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Members int    `json:"members"`
	}
	rows := make([]row, 0, len(ags))
	for _, ag := range ags {
		rows = append(rows, row{ag.ID, c.Index.GroupName(ag.ID), len(c.Index.GroupMembers[ag.ID])})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	if flagJSON {
		return writeJSON(map[string]any{"app": label, "groups": rows})
	}
	if asCSV {
		out := make([][]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, []string{r.Name, r.ID, strconv.Itoa(r.Members)})
		}
		return writeCSV([]string{"name", "id", "members"}, out)
	}
	fmt.Printf("%s — %d assigned group(s)\n\n", label, len(rows))
	t := newTable(os.Stdout)
	t.row("GROUP", "MEMBERS", "ID")
	for _, r := range rows {
		t.row(r.Name, strconv.Itoa(r.Members), r.ID)
	}
	t.flush()
	return nil
}

func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "user <user>",
		Short:             "Show which apps a user has, and why (onboarding/offboarding view)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUsers,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			u, err := c.ResolveUser(args[0])
			if err != nil {
				return err
			}
			access := c.Index.UserApps(u.ID)

			if flagJSON {
				type row struct {
					AppID  string   `json:"appId"`
					Label  string   `json:"label"`
					Status string   `json:"status"`
					Direct bool     `json:"direct"`
					Groups []string `json:"viaGroups"`
				}
				rows := make([]row, 0, len(access))
				for _, a := range access {
					var gs []string
					for _, g := range a.ViaGroups {
						gs = append(gs, g.Profile.Name)
					}
					rows = append(rows, row{a.App.ID, a.App.Label, a.Status, a.Direct, gs})
				}
				return writeJSON(map[string]any{
					"user": map[string]string{"id": u.ID, "login": u.Profile.Login,
						"name": u.Name(), "status": u.Status},
					"apps": rows,
				})
			}

			fmt.Printf("%s  <%s>  [%s]\n", u.Name(), u.Profile.Login, u.Status)
			fmt.Printf("%d app(s)\n\n", len(access))
			t := newTable(os.Stdout)
			t.row("APP", "STATUS", "ACCESS VIA")
			for _, a := range access {
				t.row(a.App.Label, a.Status, accessViaUser(a))
			}
			t.flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output JSON")
	return cmd
}

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "group <group>",
		Short:             "Show a group's members and the apps it grants",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeGroups,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := open(cmd.Context())
			if err != nil {
				return err
			}
			g, err := c.ResolveGroup(args[0])
			if err != nil {
				return err
			}

			var grants []string
			for _, a := range c.Index.Apps {
				if c.AppHasGroup(a.ID, g.ID) {
					grants = append(grants, a.Label)
				}
			}
			sort.Strings(grants)

			// Group members are only cached for app-assigned groups; fetch on
			// demand for the rest so this command works for any group.
			memberIDs, cached := c.Index.GroupMembers[g.ID]
			if !cached {
				members, err := c.Client.GroupMembers(cmd.Context(), g.ID)
				if err != nil {
					return err
				}
				for _, m := range members {
					memberIDs = append(memberIDs, m.ID)
				}
			}

			if flagJSON {
				type mrow struct {
					ID    string `json:"id"`
					Login string `json:"login"`
					Name  string `json:"name"`
				}
				rows := make([]mrow, 0, len(memberIDs))
				for _, id := range memberIDs {
					if u, ok := c.Index.User(id); ok {
						rows = append(rows, mrow{u.ID, u.Profile.Login, u.Name()})
					} else {
						rows = append(rows, mrow{ID: id})
					}
				}
				return writeJSON(map[string]any{
					"group":   map[string]string{"id": g.ID, "name": g.Profile.Name, "type": g.Type},
					"grants":  grants,
					"members": rows,
				})
			}

			fmt.Printf("%s  (%s, %s)\n", g.Profile.Name, g.ID, g.Type)
			if g.Profile.Description != "" {
				fmt.Printf("%s\n", g.Profile.Description)
			}
			if len(grants) > 0 {
				fmt.Printf("grants: %s\n", strings.Join(grants, ", "))
			} else {
				fmt.Printf("grants: (no apps)\n")
			}
			fmt.Printf("\n%d member(s)\n\n", len(memberIDs))
			t := newTable(os.Stdout)
			t.row("LOGIN", "NAME")
			ms := make([]string, 0, len(memberIDs))
			for _, id := range memberIDs {
				if u, ok := c.Index.User(id); ok {
					ms = append(ms, u.Profile.Login+"\x00"+u.Name())
				} else {
					ms = append(ms, id+"\x00")
				}
			}
			sort.Strings(ms)
			for _, m := range ms {
				parts := strings.SplitN(m, "\x00", 2)
				t.row(parts[0], parts[1])
			}
			t.flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "output JSON")
	return cmd
}

// ---- shared formatting ----------------------------------------------------

func accessVia(a cache.Assignment) string {
	var parts []string
	if a.Direct {
		parts = append(parts, "direct")
	}
	for _, g := range a.ViaGroups {
		parts = append(parts, "group:"+g.Profile.Name)
	}
	if len(parts) == 0 {
		return "group (unresolved)"
	}
	return strings.Join(parts, ", ")
}

func accessViaUser(a cache.UserAccess) string {
	return accessVia(cache.Assignment{Direct: a.Direct, ViaGroups: a.ViaGroups})
}

func groupNames(a cache.Assignment) string {
	var ns []string
	for _, g := range a.ViaGroups {
		ns = append(ns, g.Profile.Name)
	}
	return strings.Join(ns, "|")
}

func boolYN(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func matchesAny(needle string, haystacks ...string) bool {
	n := strings.ToLower(needle)
	for _, h := range haystacks {
		if strings.Contains(strings.ToLower(h), n) {
			return true
		}
	}
	return false
}
