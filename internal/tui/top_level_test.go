package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

func TestNumberKeysSwitchTopLevelViews(t *testing.T) {
	model := topLevelTestModel()
	if model.screen != screenApps {
		t.Fatalf("initial screen = %v, want Apps", model.screen)
	}

	for _, tc := range []struct {
		key  string
		want screen
	}{
		{"2", screenGroups},
		{"3", screenUsers},
		{"1", screenApps},
	} {
		model.handleKey(keyMsg(tc.key))
		if model.screen != tc.want {
			t.Errorf("key %s selected screen %v, want %v", tc.key, model.screen, tc.want)
		}
	}
}

func TestNumberKeysSwitchViewsDuringRefresh(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyRefresh

	for _, tc := range []struct {
		key  string
		want screen
	}{
		{"2", screenGroups},
		{"3", screenUsers},
		{"1", screenApps},
	} {
		model.handleKey(keyMsg(tc.key))
		if model.screen != tc.want {
			t.Errorf("key %s selected screen %v during refresh, want %v", tc.key, model.screen, tc.want)
		}
	}
}

func TestNumberKeysRemainBlockedDuringMutation(t *testing.T) {
	model := topLevelTestModel()
	model.busyKind = busyApply

	model.handleKey(keyMsg("2"))
	if model.screen != screenApps {
		t.Fatalf("screen changed to %v during mutation, want Apps", model.screen)
	}
}

func TestTopLevelFiltersRemainIndependent(t *testing.T) {
	model := topLevelTestModel()
	model.appFiltr = "alpha"
	model.groupFiltr = "platform"
	model.userFiltr = "gavin"

	model.handleKey(keyMsg("2"))
	if got := model.filteredTopGroups(); len(got) != 1 || got[0].ID != "group-1" {
		t.Fatalf("group filter returned %#v", got)
	}
	model.handleKey(keyMsg("3"))
	if got := model.filteredTopUsers(); len(got) != 1 || got[0].ID != "user-1" {
		t.Fatalf("user filter returned %#v", got)
	}
	model.handleKey(keyMsg("1"))
	if got := model.filteredApps(); len(got) != 1 || got[0].ID != "app-1" {
		t.Fatalf("app filter returned %#v", got)
	}
}

func TestGroupAndUserViewsDrillDownAndBack(t *testing.T) {
	model := topLevelTestModel()

	model.handleKey(keyMsg("2"))
	model.handleKey(keyMsg("enter"))
	// A group opens on its membership; its apps are one tab away.
	if model.screen != screenGroupMembers || len(model.members) != 1 {
		t.Fatalf("group drill-down: screen=%v members=%#v", model.screen, model.members)
	}
	model.handleKey(keyMsg("tab"))
	if model.screen != screenGroupApps || len(model.groupApps) != 1 || model.groupApps[0].ID != "app-1" {
		t.Fatalf("group apps tab: screen=%v apps=%#v", model.screen, model.groupApps)
	}
	model.handleKey(keyMsg("enter"))
	if model.screen != screenAssignments || model.assignmentBack != screenGroupApps || !model.showGroups {
		t.Fatalf("group app drill-down: screen=%v back=%v showGroups=%v",
			model.screen, model.assignmentBack, model.showGroups)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenGroupApps {
		t.Fatalf("group assignment back = %v, want group apps", model.screen)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenGroups {
		t.Fatalf("group apps back = %v, want groups", model.screen)
	}

	model.handleKey(keyMsg("3"))
	model.handleKey(keyMsg("enter"))
	if model.screen != screenUserApps || model.detailBack != screenUsers {
		t.Fatalf("user drill-down: screen=%v back=%v", model.screen, model.detailBack)
	}
	if len(model.revAccess) != 1 || model.revAccess[0].App.ID != "app-1" {
		t.Fatalf("user access = %#v", model.revAccess)
	}
	model.handleKey(keyMsg("esc"))
	if model.screen != screenUsers {
		t.Fatalf("user apps back = %v, want users", model.screen)
	}
}

func TestHeaderAlwaysShowsNumberedResourceViews(t *testing.T) {
	model := topLevelTestModel()
	for _, target := range []screen{screenApps, screenGroups, screenUsers, screenGroupApps, screenUserApps} {
		model.screen = target
		header := model.renderHeader()
		for _, label := range []string{"1 Apps", "2 Groups", "3 Users"} {
			if !strings.Contains(header, label) {
				t.Errorf("screen %v header %q does not contain %q", target, header, label)
			}
		}
	}
}

func topLevelTestModel() *Model {
	app := okta.App{ID: "app-1", Label: "Alpha Console", Name: "alpha"}
	group := okta.Group{ID: "group-1", Type: "OKTA_GROUP"}
	group.Profile.Name = "Platform Admins"
	member := okta.User{ID: "user-1", Status: "ACTIVE"}
	member.Profile.Login = "gavin@example.com"
	member.Profile.DisplayName = "Gavin"

	snapshot := cache.Empty("https://example.okta.com")
	snapshot.Apps = []okta.App{app}
	snapshot.Groups = []okta.Group{group}
	snapshot.Users = []okta.User{member}
	snapshot.AppGroups[app.ID] = []okta.AppGroup{{ID: group.ID}}
	snapshot.AppUsers[app.ID] = []okta.AppUser{{ID: member.ID, Scope: "GROUP", Status: "ACTIVE"}}
	snapshot.GroupMembers[group.ID] = []string{member.ID}

	ctx := &okxapp.Context{
		Cfg:   config.Config{OrgURL: snapshot.OrgURL},
		Index: cache.NewIndex(snapshot),
	}
	model := New(context.Background(), ctx)
	model.width = 100
	model.height = 30
	return model
}

func keyMsg(key string) tea.KeyMsg {
	if typ, ok := map[string]tea.KeyType{
		"enter":  tea.KeyEnter,
		"esc":    tea.KeyEsc,
		"ctrl+c": tea.KeyCtrlC,
	}[key]; ok {
		return tea.KeyMsg{Type: typ}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
