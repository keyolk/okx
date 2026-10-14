package tui

import (
	"testing"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/okta"
)

func TestFilteredAppsMatchesAssignedGroupAndMember(t *testing.T) {
	alpha := okta.App{ID: "app-1", Label: "Alpha Console"}
	billing := okta.App{ID: "app-2", Label: "Billing"}
	group := okta.Group{ID: "group-1"}
	group.Profile.Name = "Platform Admins"
	member := okta.User{ID: "user-1"}
	member.Profile.Login = "gavin@example.com"

	snapshot := cache.Empty("https://example.okta.com")
	snapshot.Apps = []okta.App{alpha, billing}
	snapshot.Groups = []okta.Group{group}
	snapshot.Users = []okta.User{member}
	snapshot.AppGroups[alpha.ID] = []okta.AppGroup{{ID: group.ID}}
	snapshot.AppUsers[alpha.ID] = []okta.AppUser{{ID: member.ID, Scope: "GROUP"}}

	model := &Model{
		app:  &okxapp.Context{Index: cache.NewIndex(snapshot)},
		apps: snapshot.Apps,
	}
	for _, query := range []string{"platform", "gavin"} {
		model.appFiltr = query
		got := model.filteredApps()
		if len(got) != 1 || got[0].ID != alpha.ID {
			t.Errorf("filteredApps(%q) = %#v, want only %s", query, got, alpha.ID)
		}
	}
}

// A removal is confirmed with y only; enter (the drill-in key) must not fire it.
func TestConfirmRemovalIgnoresEnter(t *testing.T) {
	model := topLevelTestModel()
	model.pending = []plannedChange{{verb: "unassign", kind: "user", id: "user-1", label: "u"}}
	model.overlay = overlayConfirm

	model.handleKey(keyMsg("enter"))
	if model.overlay != overlayConfirm || model.busyKind != busyNone {
		t.Fatalf("enter applied a removal: overlay=%v busy=%v", model.overlay, model.busyKind)
	}

	_, cmd := model.handleKey(keyMsg("y"))
	if model.busyKind != busyApply || cmd == nil {
		t.Fatalf("y did not apply the removal: busy=%v cmd=%v", model.busyKind, cmd != nil)
	}
}

// Adding access is not destructive, so enter still confirms it.
func TestConfirmAssignAcceptsEnter(t *testing.T) {
	model := topLevelTestModel()
	model.pending = []plannedChange{{verb: "assign", kind: "user", id: "user-1", label: "u"}}
	model.overlay = overlayConfirm

	if _, cmd := model.handleKey(keyMsg("enter")); model.busyKind != busyApply || cmd == nil {
		t.Fatalf("enter did not apply an assignment: busy=%v", model.busyKind)
	}
}
