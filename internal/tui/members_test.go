package tui

import (
	"context"
	"strings"
	"testing"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

// The bug this screen fixes: a group's members were never listed anywhere, and
// the group list rendered an unresolved membership as "—", which reads as zero.
func TestGroupOpensOnItsFullMembership(t *testing.T) {
	model := membersTestModel()

	openPlatformAdmins(model)
	if model.screen != screenGroupMembers {
		t.Fatalf("screen = %v, want group members", model.screen)
	}
	got := loginsOf(model.members)
	want := []string{"ann@example.com", "bob@example.com", "cara@example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("members = %v, want %v", got, want)
	}

	view := model.View()
	for _, login := range want {
		if !strings.Contains(view, login) {
			t.Errorf("member %q missing from the view:\n%s", login, view)
		}
	}
}

// A member the app never assigned still belongs to the group and must be listed.
// This is the case the old app-centric views could not represent at all.
func TestMembersIncludeUsersWithoutTheApp(t *testing.T) {
	model := membersTestModel()
	openPlatformAdmins(model)

	for _, r := range model.members {
		if r.user.Profile.Login == "cara@example.com" {
			return
		}
	}
	t.Fatalf("a group member with no app assignment was dropped: %v", loginsOf(model.members))
}

// KEEPS counts the group's apps the member also holds directly — the access
// that survives unassigning the group.
func TestKeepsCountsDirectlyHeldApps(t *testing.T) {
	model := membersTestModel()
	openPlatformAdmins(model)

	byLogin := map[string]int{}
	for _, r := range model.members {
		byLogin[r.user.Profile.Login] = r.directApps
	}
	if byLogin["ann@example.com"] != 1 {
		t.Errorf("ann keeps %d app(s), want 1 (she is assigned directly too)", byLogin["ann@example.com"])
	}
	if byLogin["bob@example.com"] != 0 {
		t.Errorf("bob keeps %d app(s), want 0 (group-only access)", byLogin["bob@example.com"])
	}
}

// An unresolved group must not render as "0 members".
func TestUnresolvedMembershipRendersAsUnknown(t *testing.T) {
	model := membersTestModel()
	if got := model.memberCountLabel("group-unfetched"); got != "?" {
		t.Errorf("member count for an unresolved group = %q, want %q", got, "?")
	}
	if got := model.memberCountLabel("group-1"); got != "3" {
		t.Errorf("member count for a resolved group = %q, want %q", got, "3")
	}

	model.handleKey(keyMsg("2"))
	if view := model.View(); !strings.Contains(view, "?") {
		t.Errorf("group list does not distinguish unresolved membership:\n%s", view)
	}
}

// Entering an unresolved group issues exactly one fetch and says so meanwhile.
func TestUnresolvedGroupFetchesOnOpen(t *testing.T) {
	model := membersTestModel()
	model.curGroup = okta.Group{ID: "group-unfetched"}

	cmd := model.openGroupMembers(screenGroups)
	if cmd == nil {
		t.Fatal("opening an unresolved group issued no fetch")
	}
	if !model.membersLoading {
		t.Fatal("membersLoading = false while the fetch is in flight")
	}
	if view := model.View(); !strings.Contains(view, "resolving members") {
		t.Errorf("loading state is not explained:\n%s", view)
	}

	// A resolved group needs no call at all.
	model.curGroup = okta.Group{ID: "group-1"}
	if cmd := model.openGroupMembers(screenGroups); cmd != nil {
		t.Error("a cached group issued a redundant fetch")
	}
}

// A reply for a group the user already navigated away from must not hijack the
// screen, but is still worth keeping in the index.
func TestLateMembersReplyForAnotherGroupIsIgnored(t *testing.T) {
	model := membersTestModel()
	model.curGroup = okta.Group{ID: "group-1"}
	model.openGroupMembers(screenGroups)
	before := len(model.members)

	model.Update(membersDoneMsg{groupID: "group-other", users: nil})
	if len(model.members) != before {
		t.Fatalf("a reply for another group replaced the list: %d → %d", before, len(model.members))
	}
}

// Members and the granted apps are two tabs of one group view; esc returns to
// wherever the group was opened from.
func TestMembersAndAppsTabTogetherAndReturnToEntryPoint(t *testing.T) {
	model := membersTestModel()
	openPlatformAdmins(model)

	model.handleKey(keyMsg("tab"))
	if model.screen != screenGroupApps {
		t.Fatalf("tab from members went to %v, want group apps", model.screen)
	}
	model.handleKey(keyMsg("tab"))
	if model.screen != screenGroupMembers {
		t.Fatalf("tab from apps went to %v, want group members", model.screen)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenGroups {
		t.Fatalf("esc went to %v, want the group list", model.screen)
	}

	// Entered from an app's groups tab, esc must go back there, not to Groups.
	model.handleKey(keyMsg("esc"))
	model.handleKey(keyMsg("1"))
	model.handleKey(keyMsg("enter")) // app → assignments
	model.handleKey(keyMsg("tab"))   // users → groups
	model.handleKey(keyMsg("enter")) // group → members
	if model.screen != screenGroupMembers {
		t.Fatalf("app groups tab drill-down: screen = %v, want group members", model.screen)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenAssignments {
		t.Fatalf("esc from members went to %v, want the app's assignments", model.screen)
	}
}

// Filtering is per-screen; the member filter must not leak into the group list.
func TestMemberFilterIsIndependent(t *testing.T) {
	model := membersTestModel()
	openPlatformAdmins(model)

	model.handleKey(keyMsg("/"))
	for _, r := range "bob" {
		model.handleKey(keyMsg(string(r)))
	}
	model.handleKey(keyMsg("enter"))
	if got := loginsOf(model.filteredMembers()); len(got) != 1 || got[0] != "bob@example.com" {
		t.Fatalf("filtered members = %v, want only bob", got)
	}
	if model.groupFiltr != "" {
		t.Errorf("member filter leaked into the group list: %q", model.groupFiltr)
	}
}

// Drilling into a member reaches the reverse view and comes back.
func TestMemberDrillsIntoUserApps(t *testing.T) {
	model := membersTestModel()
	openPlatformAdmins(model)
	model.handleKey(keyMsg("enter"))
	if model.screen != screenUserApps || model.detailBack != screenGroupMembers {
		t.Fatalf("member drill-down: screen=%v back=%v", model.screen, model.detailBack)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenGroupMembers {
		t.Fatalf("back from user apps = %v, want group members", model.screen)
	}
}

// openPlatformAdmins selects the resolved group and opens it. "All Sendbirdian"
// sorts first and is deliberately left unresolved, so the row matters.
func openPlatformAdmins(m *Model) {
	m.handleKey(keyMsg("2"))
	m.handleKey(keyMsg("j"))
	if got := m.filteredTopGroups()[m.groupCur].ID; got != "group-1" {
		panic("test setup: cursor is on " + got + ", want group-1")
	}
	m.handleKey(keyMsg("enter"))
}

func loginsOf(rows []memberRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.user.Profile.Login
	}
	return out
}

// membersTestModel builds a group of three where only two hold the app: one of
// them directly as well, one purely through the group, and one not at all.
func membersTestModel() *Model {
	app := okta.App{ID: "app-1", Label: "Alpha Console", Name: "alpha"}
	group := okta.Group{ID: "group-1", Type: "OKTA_GROUP"}
	group.Profile.Name = "Platform Admins"
	unfetched := okta.Group{ID: "group-unfetched", Type: "OKTA_GROUP"}
	unfetched.Profile.Name = "All Sendbirdian"

	user := func(id, login, name string) okta.User {
		u := okta.User{ID: id, Status: "ACTIVE"}
		u.Profile.Login = login
		u.Profile.DisplayName = name
		return u
	}
	ann := user("user-ann", "ann@example.com", "Ann")
	bob := user("user-bob", "bob@example.com", "Bob")
	cara := user("user-cara", "cara@example.com", "Cara")

	snapshot := cache.Empty("https://example.okta.com")
	snapshot.Apps = []okta.App{app}
	snapshot.Groups = []okta.Group{group, unfetched}
	snapshot.Users = []okta.User{ann, bob, cara}
	snapshot.AppGroups[app.ID] = []okta.AppGroup{{ID: group.ID}}
	snapshot.AppUsers[app.ID] = []okta.AppUser{
		{ID: ann.ID, Scope: "USER", Status: "ACTIVE"},
		{ID: bob.ID, Scope: "GROUP", Status: "ACTIVE"},
	}
	// cara is in the group but the app never assigned her.
	snapshot.GroupMembers[group.ID] = []string{ann.ID, bob.ID, cara.ID}

	ctx := &okxapp.Context{
		Cfg:   config.Config{OrgURL: snapshot.OrgURL},
		Index: cache.NewIndex(snapshot),
	}
	model := New(context.Background(), ctx)
	model.width = 100
	model.height = 30
	return model
}
