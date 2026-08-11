package cli

import (
	"strings"

	"github.com/spf13/cobra"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/fuzzy"
)

// Shell completion runs on every Tab press, so it must never block on the
// network: it uses whatever snapshot is on disk and returns nothing if there
// isn't one. `okx refresh` is what populates it.
func completionContext(cmd *cobra.Command) *okxapp.Context {
	c, err := okxapp.Open(cmd.Context(), okxapp.Options{
		ConfigPath: flagConfig,
		CacheOnly:  true,
		Quiet:      true,
	})
	if err != nil {
		return nil
	}
	return c
}

const maxCompletions = 50

// rank filters candidates by the fuzzy score against what the user has typed.
func rank(candidates []string, prefix string) []string {
	if prefix == "" {
		if len(candidates) > maxCompletions {
			return candidates[:maxCompletions]
		}
		return candidates
	}
	matches := fuzzy.Filter(candidates, prefix)
	out := make([]string, 0, len(matches))
	for i, m := range matches {
		if i >= maxCompletions {
			break
		}
		out = append(out, candidates[m.Index])
	}
	return out
}

// completionItem formats "value\tdescription", which cobra renders as a hint.
func completionItem(value, desc string) string {
	// A completion value containing spaces has to be quotable by the shell;
	// cobra handles the escaping, but a tab inside the value would break the
	// description split.
	return strings.ReplaceAll(value, "\t", " ") + "\t" + desc
}

func completeApps(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	labels := make([]string, 0, len(c.Index.Apps))
	descs := make(map[string]string, len(c.Index.Apps))
	for _, a := range c.Index.Apps {
		labels = append(labels, a.Label)
		descs[a.Label] = a.SignOnMode
	}
	ranked := rank(labels, toComplete)
	out := make([]string, 0, len(ranked))
	for _, l := range ranked {
		out = append(out, completionItem(l, descs[l]))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeUsers(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	logins := make([]string, 0, len(c.Index.Users))
	descs := make(map[string]string, len(c.Index.Users))
	for _, u := range c.Index.Users {
		logins = append(logins, u.Profile.Login)
		descs[u.Profile.Login] = u.Name()
	}
	ranked := rank(logins, toComplete)
	out := make([]string, 0, len(ranked))
	for _, l := range ranked {
		out = append(out, completionItem(l, descs[l]))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeGroups(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(c.Index.Groups))
	for _, g := range c.Index.Groups {
		names = append(names, g.Profile.Name)
	}
	ranked := rank(names, toComplete)
	out := make([]string, 0, len(ranked))
	for _, n := range ranked {
		out = append(out, completionItem(n, ""))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeAssignArgs completes the app in position 0, then users or groups.
func completeAssignArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeApps(cmd, args, toComplete)
	}
	if flagGroup {
		return completeUnassignedGroups(cmd, args[0], toComplete)
	}
	return completeUnassignedUsers(cmd, args[0], toComplete)
}

// completeUnassignArgs completes only targets that are actually assigned,
// which is the whole point: you cannot unassign what is not there.
func completeUnassignArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeApps(cmd, args, toComplete)
	}
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	app, err := c.ResolveApp(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var values []string
	descs := map[string]string{}
	if flagGroup {
		for _, ag := range c.Index.AppGroups[app.ID] {
			name := c.Index.GroupName(ag.ID)
			values = append(values, name)
			descs[name] = "assigned group"
		}
	} else {
		for _, a := range c.Assignments(app.ID) {
			// Only direct assignments can be removed with `unassign <user>`.
			if !a.Direct {
				continue
			}
			values = append(values, a.User.Profile.Login)
			descs[a.User.Profile.Login] = a.User.Name()
		}
	}
	ranked := rank(values, toComplete)
	out := make([]string, 0, len(ranked))
	for _, v := range ranked {
		out = append(out, completionItem(v, descs[v]))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeUnassignedUsers(cmd *cobra.Command, appQuery, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	app, err := c.ResolveApp(appQuery)
	if err != nil {
		return completeUsers(cmd, nil, toComplete)
	}
	has := map[string]bool{}
	for _, au := range c.Index.AppUsers[app.ID] {
		if au.Scope == "USER" {
			has[au.ID] = true
		}
	}
	var values []string
	descs := map[string]string{}
	for _, u := range c.Index.Users {
		if has[u.ID] {
			continue // already directly assigned; assigning again is a no-op
		}
		values = append(values, u.Profile.Login)
		descs[u.Profile.Login] = u.Name()
	}
	ranked := rank(values, toComplete)
	out := make([]string, 0, len(ranked))
	for _, v := range ranked {
		out = append(out, completionItem(v, descs[v]))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeUnassignedGroups(cmd *cobra.Command, appQuery, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := completionContext(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	app, err := c.ResolveApp(appQuery)
	if err != nil {
		return completeGroups(cmd, nil, toComplete)
	}
	has := map[string]bool{}
	for _, ag := range c.Index.AppGroups[app.ID] {
		has[ag.ID] = true
	}
	var values []string
	for _, g := range c.Index.Groups {
		if has[g.ID] {
			continue
		}
		values = append(values, g.Profile.Name)
	}
	ranked := rank(values, toComplete)
	out := make([]string, 0, len(ranked))
	for _, v := range ranked {
		out = append(out, completionItem(v, ""))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
