package tui

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/keyolk/okx/internal/cache"
	"github.com/keyolk/okx/internal/okta"
)

// The member fetch runs on a Bubble Tea goroutine while View keeps reading the
// index. It used to write GroupMembers/userByID from that goroutine, which the
// race detector (and occasionally the runtime) flags as a concurrent map write.
// Run with -race to make this meaningful.
func TestMemberFetchDoesNotWriteIndexOffThread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"user-new","status":"ACTIVE","profile":{"login":"new@example.com"}}]`))
	}))
	defer srv.Close()

	model := membersTestModel()
	model.app.Client = okta.New(srv.URL, "token")
	model.curGroup = okta.Group{ID: "group-unfetched"}
	model.handleKey(keyMsg("2"))
	cmd := model.openGroupMembers(screenGroups)
	if cmd == nil {
		t.Fatal("opening an unresolved group issued no fetch")
	}
	model.screen = screenGroups // the user went back while the fetch runs

	var wg sync.WaitGroup
	done := make(chan struct{})
	var msg any
	wg.Add(1)
	go func() {
		defer wg.Done()
		msg = cmd()
		close(done)
	}()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			_ = model.View() // reads GroupMembers via memberCountLabel
		}
	}
	wg.Wait()

	if model.app.Index.HasMembers("group-unfetched") {
		t.Fatal("the fetch goroutine wrote the index; only Update may")
	}
	model.Update(msg)
	if got := model.memberCountLabel("group-unfetched"); got != "1" {
		t.Fatalf("member count after the reply = %q, want 1", got)
	}
}

// A refresh can swap the index while a member fetch is in flight. The reply
// must land in the index that is current when it arrives, not the old one.
func TestMembersReplyLandsInCurrentIndex(t *testing.T) {
	model := membersTestModel()
	model.curGroup = okta.Group{ID: "group-unfetched"}
	model.openGroupMembers(screenGroups)

	old := model.app.Index
	snap := cache.Empty("https://example.okta.com")
	snap.Groups = []okta.Group{model.curGroup}
	model.app.Index = cache.NewIndex(snap)

	u := okta.User{ID: "user-new", Status: "ACTIVE"}
	u.Profile.Login = "new@example.com"
	model.Update(membersDoneMsg{groupID: "group-unfetched", users: []okta.User{u}})

	if !model.app.Index.HasMembers("group-unfetched") {
		t.Fatal("reply was not folded into the current index")
	}
	if old.HasMembers("group-unfetched") {
		t.Fatal("reply was written into the superseded index")
	}
	if got := loginsOf(model.members); len(got) != 1 || got[0] != "new@example.com" {
		t.Fatalf("members = %v, want the fetched user", got)
	}
}
