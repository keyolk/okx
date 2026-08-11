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
