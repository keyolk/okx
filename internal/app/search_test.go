package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/config"
	"github.com/keyolk/okx/internal/okta"
)

func TestDefaultTTLIsOneWeek(t *testing.T) {
	if DefaultTTL != 7*24*time.Hour {
		t.Fatalf("DefaultTTL = %s, want 168h", DefaultTTL)
	}
}

func TestSearchFindsAppsGroupsAndMembers(t *testing.T) {
	c := searchTestContext()

	results := c.Search("alpha")
	if len(results) != 3 {
		t.Fatalf("Search(alpha) returned %d results, want 3: %#v", len(results), results)
	}

	got := map[string]string{}
	for _, result := range results {
		got[result.Kind] = result.Label
	}
	want := map[string]string{
		"app":    "Alpha Console",
		"group":  "Platform Admins",
		"member": "gavin@example.com",
	}
	for kind, label := range want {
		if got[kind] != label {
			t.Errorf("%s result = %q, want %q", kind, got[kind], label)
		}
	}
}

func TestSearchNoMatchesReturnsEmptySlice(t *testing.T) {
	results := searchTestContext().Search("zzzzzz")
	if results == nil {
		t.Fatal("Search returned nil, want an empty slice for stable JSON output")
	}
	if len(results) != 0 {
		t.Fatalf("Search returned %d results, want none", len(results))
	}
}

func TestAppMatchesAssignedGroupOrMember(t *testing.T) {
	c := searchTestContext()

	for _, query := range []string{"platform", "gavin", "Alpha Console"} {
		if !c.AppMatches("app-1", query) {
			t.Errorf("AppMatches(app-1, %q) = false, want true", query)
		}
	}
	if c.AppMatches("app-2", "platform") {
		t.Fatal("AppMatches(app-2, platform) = true for an unrelated app")
	}
}

func TestOpenCacheOnlyAcceptsStaleSnapshot(t *testing.T) {
	cfg := testConfig(t, "https://cache-only.invalid", "cache-only-token")
	snapshot := cache.Empty(cfg.OrgURL)
	snapshot.FetchedAt = time.Now().Add(-30 * 24 * time.Hour)
	snapshot.Apps = []okta.App{{ID: "cached-app", Label: "Cached App"}}
	if err := cache.Save(cache.Path(cfg.CacheKey()), snapshot); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	c, err := Open(context.Background(), Options{CacheOnly: true, TTL: time.Hour})
	if err != nil {
		t.Fatalf("Open cache-only: %v", err)
	}
	if !c.NeedsRefresh {
		t.Fatal("NeedsRefresh = false for a stale cache")
	}
	if len(c.Index.Apps) != 1 || c.Index.Apps[0].ID != "cached-app" {
		t.Fatalf("loaded apps = %#v, want cached-app", c.Index.Apps)
	}
}

func TestOpenDefersInitialFetchForTUI(t *testing.T) {
	testConfig(t, "https://deferred.invalid", "deferred-token")

	c, err := Open(context.Background(), Options{AllowStale: true, DeferFetch: true})
	if err != nil {
		t.Fatalf("Open deferred: %v", err)
	}
	if !c.NeedsRefresh {
		t.Fatal("NeedsRefresh = false without a cache")
	}
	if len(c.Index.Apps) != 0 || len(c.Index.Users) != 0 || len(c.Index.Groups) != 0 {
		t.Fatalf("deferred snapshot is not empty: %#v", c.Index.Snapshot)
	}
}

func TestOpenRefreshRefetchesExistingSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps":
			fmt.Fprint(w, `[{"id":"fresh-app","name":"fresh","label":"Fresh App"}]`)
		case "/api/v1/users", "/api/v1/groups",
			"/api/v1/apps/fresh-app/users", "/api/v1/apps/fresh-app/groups":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL, "refresh-token")
	old := cache.Empty(cfg.OrgURL)
	old.FetchedAt = time.Now()
	old.Apps = []okta.App{{ID: "old-app", Label: "Old App"}}
	if err := cache.Save(cache.Path(cfg.CacheKey()), old); err != nil {
		t.Fatalf("save old cache: %v", err)
	}

	c, err := Open(context.Background(), Options{Refresh: true, Quiet: true})
	if err != nil {
		t.Fatalf("Open refresh: %v", err)
	}
	if c.NeedsRefresh {
		t.Fatal("NeedsRefresh = true after successful refresh")
	}
	if len(c.Index.Apps) != 1 || c.Index.Apps[0].ID != "fresh-app" {
		t.Fatalf("refreshed apps = %#v, want fresh-app", c.Index.Apps)
	}
}

func testConfig(t *testing.T, orgURL, token string) config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OKTA_ORG_URL", orgURL)
	t.Setenv("OKTA_API_TOKEN", token)
	return config.Config{OrgURL: orgURL, Token: token}
}

func searchTestContext() *Context {
	alphaApp := okta.App{ID: "app-1", Label: "Alpha Console", Name: "alpha-console", SignOnMode: "SAML_2_0"}
	otherApp := okta.App{ID: "app-2", Label: "Billing", Name: "billing"}

	group := okta.Group{ID: "group-1", Type: "OKTA_GROUP"}
	group.Profile.Name = "Platform Admins"
	group.Profile.Description = "Alpha operators"

	member := okta.User{ID: "user-1", Status: "ACTIVE"}
	member.Profile.Login = "gavin@example.com"
	member.Profile.Email = "gavin@example.com"
	member.Profile.DisplayName = "Alpha Owner"

	snapshot := cache.Empty("https://example.okta.com")
	snapshot.Apps = []okta.App{alphaApp, otherApp}
	snapshot.Groups = []okta.Group{group}
	snapshot.Users = []okta.User{member}
	snapshot.AppGroups[alphaApp.ID] = []okta.AppGroup{{ID: group.ID}}
	snapshot.AppUsers[alphaApp.ID] = []okta.AppUser{{ID: member.ID, Scope: "GROUP"}}

	return &Context{Index: cache.NewIndex(snapshot)}
}
