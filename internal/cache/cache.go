// Package cache keeps a local snapshot of the org's apps, users, groups and
// app assignments.
//
// Okta rate-limits /api/v1/apps at 50 requests/minute, and answering "why does
// this user have this app?" needs the members of every group assigned to the
// app. Recomputing that per keystroke is not viable, so we snapshot once and
// refresh explicitly.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/keyolk/okx/internal/okta"
)

// Snapshot is the full local view of the org.
type Snapshot struct {
	FetchedAt time.Time `json:"fetchedAt"`
	OrgURL    string    `json:"orgUrl"`

	Apps   []okta.App   `json:"apps"`
	Users  []okta.User  `json:"users"`
	Groups []okta.Group `json:"groups"`

	// AppUsers and AppGroups are keyed by app ID.
	AppUsers  map[string][]okta.AppUser  `json:"appUsers"`
	AppGroups map[string][]okta.AppGroup `json:"appGroups"`

	// GroupMembers is keyed by group ID and only populated for groups that are
	// assigned to at least one app — the rest are irrelevant to attribution and
	// fetching all 487 of them would waste a minute of rate limit.
	GroupMembers map[string][]string `json:"groupMembers"`
}

// Age reports how stale the snapshot is.
func (s *Snapshot) Age() time.Duration { return time.Since(s.FetchedAt) }

// ---- lookup helpers -------------------------------------------------------

// Index provides O(1) lookups over a Snapshot.
type Index struct {
	*Snapshot
	userByID  map[string]okta.User
	groupByID map[string]okta.Group
	appByID   map[string]okta.App
}

// NewIndex builds lookup maps over a snapshot.
func NewIndex(s *Snapshot) *Index {
	ix := &Index{
		Snapshot:  s,
		userByID:  make(map[string]okta.User, len(s.Users)),
		groupByID: make(map[string]okta.Group, len(s.Groups)),
		appByID:   make(map[string]okta.App, len(s.Apps)),
	}
	for _, u := range s.Users {
		ix.userByID[u.ID] = u
	}
	for _, g := range s.Groups {
		ix.groupByID[g.ID] = g
	}
	for _, a := range s.Apps {
		ix.appByID[a.ID] = a
	}
	return ix
}

// User looks up a user by ID.
func (ix *Index) User(id string) (okta.User, bool) { u, ok := ix.userByID[id]; return u, ok }

// Group looks up a group by ID.
func (ix *Index) Group(id string) (okta.Group, bool) { g, ok := ix.groupByID[id]; return g, ok }

// App looks up an app by ID.
func (ix *Index) App(id string) (okta.App, bool) { a, ok := ix.appByID[id]; return a, ok }

// GroupName returns a group's name, or its raw ID if the group is not in the
// snapshot (possible when a group was created after the last refresh).
func (ix *Index) GroupName(id string) string {
	if g, ok := ix.groupByID[id]; ok && g.Profile.Name != "" {
		return g.Profile.Name
	}
	return id
}

// Assignment is one user's access to one app, with the reason attached.
type Assignment struct {
	User okta.User
	App  okta.App
	// Direct is true when the user is assigned to the app individually.
	Direct bool
	// ViaGroups lists the assigned groups the user is a member of. A user can
	// be both Direct and in ViaGroups; removing the direct assignment then
	// leaves access intact, which is exactly the trap this field exists for.
	ViaGroups []okta.Group
	Status    string
}

// Reason renders a short human-readable explanation of the access.
func (a Assignment) Reason() string {
	switch {
	case a.Direct && len(a.ViaGroups) > 0:
		return fmt.Sprintf("direct + %d group(s)", len(a.ViaGroups))
	case a.Direct:
		return "direct"
	case len(a.ViaGroups) == 1:
		return "group: " + a.ViaGroups[0].Profile.Name
	case len(a.ViaGroups) > 1:
		return fmt.Sprintf("%d groups", len(a.ViaGroups))
	default:
		// Okta reported scope=GROUP but no assigned group in the snapshot
		// contains this user — usually a stale snapshot or a group the token
		// cannot read.
		return "group (unresolved)"
	}
}

// AppAssignments resolves every user with access to an app, attributing each
// one to a direct assignment, the groups that grant it, or both.
func (ix *Index) AppAssignments(appID string) []Assignment {
	app, _ := ix.appByID[appID]

	// Which assigned groups does each user belong to?
	viaGroup := make(map[string][]okta.Group)
	for _, ag := range ix.AppGroups[appID] {
		g, ok := ix.groupByID[ag.ID]
		if !ok {
			g = okta.Group{ID: ag.ID}
			g.Profile.Name = ag.ID
		}
		for _, uid := range ix.GroupMembers[ag.ID] {
			viaGroup[uid] = append(viaGroup[uid], g)
		}
	}

	out := make([]Assignment, 0, len(ix.AppUsers[appID]))
	for _, au := range ix.AppUsers[appID] {
		u, ok := ix.userByID[au.ID]
		if !ok {
			u = okta.User{ID: au.ID}
			u.Profile.Login = au.Credentials.UserName
			if u.Profile.Login == "" {
				u.Profile.Login = au.ID
			}
		}
		groups := viaGroup[au.ID]
		sort.Slice(groups, func(i, j int) bool {
			return groups[i].Profile.Name < groups[j].Profile.Name
		})
		out = append(out, Assignment{
			User:      u,
			App:       app,
			Direct:    au.Scope == "USER",
			ViaGroups: groups,
			Status:    au.Status,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].User.Profile.Login < out[j].User.Profile.Login
	})
	return out
}

// UserAccess is the reverse view: every app one user can reach.
type UserAccess struct {
	App       okta.App
	Direct    bool
	ViaGroups []okta.Group
	Status    string
}

// Reason renders the explanation, matching Assignment.Reason.
func (a UserAccess) Reason() string {
	return Assignment{Direct: a.Direct, ViaGroups: a.ViaGroups}.Reason()
}

// UserApps resolves which apps a user has, and why.
func (ix *Index) UserApps(userID string) []UserAccess {
	// Groups this user belongs to, restricted to those we cached.
	member := make(map[string]bool)
	for gid, uids := range ix.GroupMembers {
		for _, uid := range uids {
			if uid == userID {
				member[gid] = true
				break
			}
		}
	}

	var out []UserAccess
	for _, app := range ix.Apps {
		var direct bool
		var found bool
		var status string
		for _, au := range ix.AppUsers[app.ID] {
			if au.ID == userID {
				found = true
				direct = au.Scope == "USER"
				status = au.Status
				break
			}
		}
		if !found {
			continue
		}
		var groups []okta.Group
		for _, ag := range ix.AppGroups[app.ID] {
			if member[ag.ID] {
				if g, ok := ix.groupByID[ag.ID]; ok {
					groups = append(groups, g)
				}
			}
		}
		sort.Slice(groups, func(i, j int) bool {
			return groups[i].Profile.Name < groups[j].Profile.Name
		})
		out = append(out, UserAccess{App: app, Direct: direct, ViaGroups: groups, Status: status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App.Label < out[j].App.Label })
	return out
}

// ---- fetching -------------------------------------------------------------

// Progress reports refresh progress to the caller (TUI spinner, CLI stderr).
type Progress func(stage string, done, total int)

// Fetch pulls a fresh snapshot from Okta.
func Fetch(ctx context.Context, c *okta.Client, orgURL string, progress Progress) (*Snapshot, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	s := &Snapshot{
		OrgURL:       orgURL,
		AppUsers:     map[string][]okta.AppUser{},
		AppGroups:    map[string][]okta.AppGroup{},
		GroupMembers: map[string][]string{},
	}

	progress("apps", 0, 0)
	apps, err := c.Apps(ctx)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	s.Apps = apps

	progress("users", 0, 0)
	users, err := c.Users(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	s.Users = users

	progress("groups", 0, 0)
	groups, err := c.Groups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	s.Groups = groups

	// Per-app assignments, in parallel but bounded: Okta's /apps rate limit is
	// the tightest in the org and a burst here is what trips it.
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, 4)
		errs []error
		done int
	)
	for _, app := range apps {
		wg.Add(1)
		go func(app okta.App) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			au, uerr := c.AppUsers(ctx, app.ID)
			ag, gerr := c.AppGroups(ctx, app.ID)

			mu.Lock()
			defer mu.Unlock()
			if uerr != nil {
				errs = append(errs, fmt.Errorf("app users %s: %w", app.Label, uerr))
			} else {
				s.AppUsers[app.ID] = au
			}
			if gerr != nil {
				errs = append(errs, fmt.Errorf("app groups %s: %w", app.Label, gerr))
			} else {
				s.AppGroups[app.ID] = ag
			}
			done++
			progress("assignments", done, len(apps))
		}(app)
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}

	// Only groups actually assigned to an app need their members resolved.
	needed := map[string]bool{}
	for _, ags := range s.AppGroups {
		for _, ag := range ags {
			needed[ag.ID] = true
		}
	}
	gids := make([]string, 0, len(needed))
	for id := range needed {
		gids = append(gids, id)
	}
	sort.Strings(gids)

	done = 0
	for _, gid := range gids {
		wg.Add(1)
		go func(gid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			members, err := c.GroupMembers(ctx, gid)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// A group we cannot read is a gap in attribution, not a fatal
				// error — the affected users show "group (unresolved)".
				errs = append(errs, fmt.Errorf("group members %s: %w", gid, err))
			} else {
				ids := make([]string, len(members))
				for i, m := range members {
					ids[i] = m.ID
				}
				s.GroupMembers[gid] = ids
			}
			done++
			progress("group members", done, len(gids))
		}(gid)
	}
	wg.Wait()

	s.FetchedAt = time.Now()
	return s, nil
}

// ---- persistence ----------------------------------------------------------

// Path returns the on-disk cache location for an org.
func Path(orgName string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = filepath.Join(os.TempDir(), "okx")
	}
	return filepath.Join(base, "okx", orgName+".json")
}

// Save writes the snapshot to disk atomically.
func Save(path string, s *Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a snapshot from disk. A missing cache returns (nil, nil).
func Load(path string) (*Snapshot, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		// A corrupt cache should not brick the tool; treat it as absent.
		return nil, nil
	}
	return &s, nil
}
