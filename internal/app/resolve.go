package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/fuzzy"
	"github.com/keyolk/okx/internal/okta"
)

// AmbiguousError reports that a query matched several candidates. Commands
// print the candidates so the user can retype with something more specific
// rather than guess.
type AmbiguousError struct {
	Kind       string
	Query      string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s %q is ambiguous:\n  %s",
		e.Kind, e.Query, strings.Join(e.Candidates, "\n  "))
}

// NotFoundError reports that nothing matched a query.
type NotFoundError struct {
	Kind  string
	Query string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s matches %q", e.Kind, e.Query)
}

const maxCandidatesShown = 10

// resolve is the shared shape of every lookup: exact ID, then exact
// case-insensitive name, then unique fuzzy match.
func resolve[T any](kind, query string, items []T, id func(T) string, names func(T) []string) (T, error) {
	var zero T
	if query == "" {
		return zero, &NotFoundError{Kind: kind, Query: query}
	}

	for _, it := range items {
		if id(it) == query {
			return it, nil
		}
	}

	q := strings.ToLower(query)
	var exact []T
	for _, it := range items {
		for _, n := range names(it) {
			if strings.ToLower(n) == q {
				exact = append(exact, it)
				break
			}
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return zero, ambiguous(kind, query, exact, id, names)
	}

	// Fuzzy: score each item by its best-scoring name.
	type scored struct {
		item  T
		score int
	}
	var hits []scored
	for _, it := range items {
		best := 0
		matched := false
		for _, n := range names(it) {
			if s, _, ok := fuzzy.Score(n, query); ok && (!matched || s > best) {
				best, matched = s, true
			}
		}
		if matched {
			hits = append(hits, scored{it, best})
		}
	}
	if len(hits) == 0 {
		return zero, &NotFoundError{Kind: kind, Query: query}
	}
	if len(hits) == 1 {
		return hits[0].item, nil
	}

	// A clearly dominant match is treated as the answer; a close field is not.
	bestIdx := 0
	for i, h := range hits {
		if h.score > hits[bestIdx].score {
			bestIdx = i
		}
	}
	runnerUp := -1 << 30
	for i, h := range hits {
		if i != bestIdx && h.score > runnerUp {
			runnerUp = h.score
		}
	}
	if hits[bestIdx].score >= runnerUp+16 {
		return hits[bestIdx].item, nil
	}

	items2 := make([]T, len(hits))
	for i, h := range hits {
		items2[i] = h.item
	}
	return zero, ambiguous(kind, query, items2, id, names)
}

func ambiguous[T any](kind, query string, items []T, id func(T) string, names func(T) []string) error {
	var cands []string
	for i, it := range items {
		if i >= maxCandidatesShown {
			cands = append(cands, fmt.Sprintf("… and %d more", len(items)-maxCandidatesShown))
			break
		}
		n := names(it)
		label := id(it)
		if len(n) > 0 {
			label = fmt.Sprintf("%s  (%s)", n[0], id(it))
		}
		cands = append(cands, label)
	}
	return &AmbiguousError{Kind: kind, Query: query, Candidates: cands}
}

// ResolveApp finds an app by ID, label, name, or fuzzy match.
func (c *Context) ResolveApp(query string) (okta.App, error) {
	return resolve("app", query, c.Index.Apps,
		func(a okta.App) string { return a.ID },
		func(a okta.App) []string { return []string{a.Label, a.Name} })
}

// ResolveUser finds a user by ID, login, email, display name, or fuzzy match.
func (c *Context) ResolveUser(query string) (okta.User, error) {
	return resolve("user", query, c.Index.Users,
		func(u okta.User) string { return u.ID },
		func(u okta.User) []string {
			return []string{u.Profile.Login, u.Profile.Email, u.Name()}
		})
}

// ResolveGroup finds a group by ID, name, or fuzzy match.
func (c *Context) ResolveGroup(query string) (okta.Group, error) {
	return resolve("group", query, c.Index.Groups,
		func(g okta.Group) string { return g.ID },
		func(g okta.Group) []string { return []string{g.Profile.Name} })
}

// AppUserScope reports how a user currently holds an app: direct, via groups,
// or not at all. Commands use it to skip no-op writes and to warn when
// removing a direct assignment leaves group-derived access behind.
func (c *Context) AppUserScope(appID, userID string) (found bool, direct bool, viaGroups []okta.Group) {
	for _, a := range c.Index.AppAssignments(appID) {
		if a.User.ID == userID {
			return true, a.Direct, a.ViaGroups
		}
	}
	return false, false, nil
}

// AppHasGroup reports whether a group is already assigned to an app.
func (c *Context) AppHasGroup(appID, groupID string) bool {
	for _, ag := range c.Index.AppGroups[appID] {
		if ag.ID == groupID {
			return true
		}
	}
	return false
}

// LoadGroupMembers resolves one group's membership on demand and folds it into
// the current index. The snapshot only pre-resolves groups assigned to an app
// (see cache.Fetch), so browsing any other group needs this one extra call —
// far cheaper than fetching all 500-odd groups up front against a 50 req/min
// rate limit.
func (c *Context) LoadGroupMembers(ctx context.Context, groupID string) ([]okta.User, error) {
	if c.Index.HasMembers(groupID) {
		return c.Index.Members(groupID), nil
	}
	members, err := c.Client.GroupMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("group members: %w", err)
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	c.Index.SetMembers(groupID, ids)
	// A member who joined after the last snapshot is not in Users yet; keeping
	// the fetched records means the list shows a login instead of a bare ID.
	c.Index.AddUsers(members)
	return c.Index.Members(groupID), nil
}

// Assignments is a convenience passthrough to the index.
func (c *Context) Assignments(appID string) []cache.Assignment {
	return c.Index.AppAssignments(appID)
}
