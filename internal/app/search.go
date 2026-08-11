package app

import (
	"sort"
	"strings"

	"github.com/keyolk/okx/internal/fuzzy"
	"github.com/keyolk/okx/internal/okta"
)

// SearchResult is one app, group, or member matched in the cached snapshot.
type SearchResult struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	score  int
}

// Search finds apps, groups, and members (Okta users) in one query.
func (c *Context) Search(query string) []SearchResult {
	out := make([]SearchResult, 0)
	for _, a := range c.Index.Apps {
		if score, ok := bestScore(query, a.ID, a.Label, a.Name); ok {
			out = append(out, SearchResult{
				Kind: "app", ID: a.ID, Label: a.Label, Detail: a.SignOnMode, score: score,
			})
		}
	}
	for _, g := range c.Index.Groups {
		if score, ok := bestScore(query, g.ID, g.Profile.Name, g.Profile.Description); ok {
			out = append(out, SearchResult{
				Kind: "group", ID: g.ID, Label: g.Profile.Name, Detail: g.Profile.Description, score: score,
			})
		}
	}
	for _, u := range c.Index.Users {
		if score, ok := bestScore(query, u.ID, u.Profile.Login, u.Profile.Email, u.Name()); ok {
			out = append(out, SearchResult{
				Kind: "member", ID: u.ID, Label: u.Profile.Login, Detail: u.Name(), score: score,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// AppMatches reports whether an app itself, one of its assigned groups, or one
// of its assigned members matches query. This lets the TUI app list answer
// questions such as “which app contains this group/member?” directly.
func (c *Context) AppMatches(appID, query string) bool {
	a, ok := c.Index.App(appID)
	if !ok {
		return false
	}
	if _, ok := bestScore(query, a.ID, a.Label, a.Name); ok {
		return true
	}
	for _, ag := range c.Index.AppGroups[appID] {
		g, ok := c.Index.Group(ag.ID)
		if ok {
			if _, matched := bestScore(query, g.ID, g.Profile.Name, g.Profile.Description); matched {
				return true
			}
		}
	}
	for _, au := range c.Index.AppUsers[appID] {
		u, ok := c.Index.User(au.ID)
		if ok && userMatches(u, query) {
			return true
		}
	}
	return false
}

func userMatches(u okta.User, query string) bool {
	_, ok := bestScore(query, u.ID, u.Profile.Login, u.Profile.Email, u.Name())
	return ok
}

func bestScore(query string, fields ...string) (int, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, true
	}
	best := 0
	matched := false
	for _, field := range fields {
		if field == "" {
			continue
		}
		score, _, ok := fuzzy.Score(field, query)
		if ok && (!matched || score > best) {
			best, matched = score, true
		}
	}
	return best, matched
}
