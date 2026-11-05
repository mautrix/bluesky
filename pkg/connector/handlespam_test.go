package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/xrpc"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

const (
	meDID    = "did:plc:me"
	themDID  = "did:plc:spammer"
	convoID  = "3convo"
	blockURI = "at://did:plc:me/app.bsky.graph.block/3blockrkey"
)

type call struct {
	Method, Proxy string
	Body          map[string]any
}

// fakePDS answers the XRPC methods the spam handlers use from memory; nothing
// reaches Bluesky.
func fakePDS(t *testing.T, blocking string) (*BlueskyClient, *[]call) {
	t.Helper()
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[len("/xrpc/"):]
		c := call{Method: method, Proxy: r.Header.Get("Atproto-Proxy")}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &c.Body)
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "chat.bsky.convo.getConvo":
			_, _ = io.WriteString(w, `{"convo":{"id":"`+convoID+`","rev":"1","muted":false,"unreadCount":0,"members":[{"did":"`+meDID+`","handle":"me.test"},{"did":"`+themDID+`","handle":"spam.test"}]}}`)
		case "app.bsky.actor.getProfile":
			viewer := `{}`
			if blocking != "" {
				viewer = `{"blocking":"` + blocking + `"}`
			}
			_, _ = io.WriteString(w, `{"did":"`+themDID+`","handle":"spam.test","viewer":`+viewer+`}`)
		case "com.atproto.repo.createRecord":
			_, _ = io.WriteString(w, `{"uri":"`+blockURI+`","cid":"bafy"}`)
		case "com.atproto.repo.deleteRecord", "chat.bsky.convo.leaveConvo":
			_, _ = io.WriteString(w, `{"convoId":"`+convoID+`","rev":"2"}`)
		case "com.atproto.moderation.createReport":
			_, _ = io.WriteString(w, `{"id":1,"reasonType":"com.atproto.moderation.defs#reasonSpam","subject":{"$type":"com.atproto.admin.defs#repoRef","did":"`+themDID+`"},"reportedBy":"`+meDID+`","createdAt":"2026-10-30T00:00:00Z"}`)
		default:
			t.Errorf("unexpected XRPC call %s", method)
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)
	auth := &xrpc.AuthInfo{AccessJwt: "a", RefreshJwt: "r", Did: meDID, Handle: "me.test"}
	ua := "test"
	b := &BlueskyClient{
		XRPC:    &xrpc.Client{Client: srv.Client(), Host: srv.URL, Auth: auth, UserAgent: &ua},
		ChatRPC: &xrpc.Client{Client: srv.Client(), Host: srv.URL, Auth: auth, UserAgent: &ua},
	}
	return b, &calls
}

func dmPortal() *bridgev2.Portal {
	return &bridgev2.Portal{Portal: &database.Portal{PortalKey: networkid.PortalKey{ID: convoID}, RoomType: database.RoomTypeDM}}
}

func methods(calls []call) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}

func TestBlockAndReportSpam(t *testing.T) {
	b, calls := fakePDS(t, "")
	err := b.HandleMatrixBlockUser(context.Background(), &bridgev2.MatrixBlockUser{
		Portal: dmPortal(), Content: &event.BeeperBlockUserEventContent{Block: true, ReportSpam: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := methods(*calls)
	want := []string{"chat.bsky.convo.getConvo", "app.bsky.actor.getProfile", "com.atproto.repo.createRecord", "com.atproto.moderation.createReport"}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
	rec := (*calls)[2].Body
	if rec["collection"] != "app.bsky.graph.block" || rec["repo"] != meDID {
		t.Fatalf("createRecord body = %v", rec)
	}
	if r := rec["record"].(map[string]any); r["subject"] != themDID || r["$type"] != "app.bsky.graph.block" {
		t.Fatalf("block record = %v", r)
	}
	rep := (*calls)[3]
	if rep.Proxy != BlueskyModerationProxy {
		t.Fatalf("createReport proxy = %q", rep.Proxy)
	}
	if rep.Body["reasonType"] != "com.atproto.moderation.defs#reasonSpam" {
		t.Fatalf("createReport body = %v", rep.Body)
	}
	if s := rep.Body["subject"].(map[string]any); s["did"] != themDID {
		t.Fatalf("report subject = %v", s)
	}
}

func TestBlockIsIdempotentAndUnblockDeletesRecord(t *testing.T) {
	b, calls := fakePDS(t, blockURI)
	if err := b.HandleMatrixBlockUser(context.Background(), &bridgev2.MatrixBlockUser{
		Portal: dmPortal(), Content: &event.BeeperBlockUserEventContent{Block: true},
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range methods(*calls) {
		if m == "com.atproto.repo.createRecord" {
			t.Fatal("an existing block must not be duplicated")
		}
	}
	*calls = nil
	if err := b.HandleMatrixBlockUser(context.Background(), &bridgev2.MatrixBlockUser{
		Portal: dmPortal(), Content: &event.BeeperBlockUserEventContent{Block: false},
	}); err != nil {
		t.Fatal(err)
	}
	last := (*calls)[len(*calls)-1]
	if last.Method != "com.atproto.repo.deleteRecord" || last.Body["rkey"] != "3blockrkey" || last.Body["repo"] != meDID {
		t.Fatalf("unblock call = %+v", last)
	}
}

func TestDeleteChatLeavesConvo(t *testing.T) {
	b, calls := fakePDS(t, "")
	if err := b.HandleMatrixDeleteChat(context.Background(), &bridgev2.MatrixDeleteChat{
		Portal: dmPortal(), Content: &event.BeeperChatDeleteEventContent{},
	}); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.Method != "chat.bsky.convo.leaveConvo" || c.Body["convoId"] != convoID {
		t.Fatalf("call = %+v", c)
	}
	if err := b.HandleMatrixDeleteChat(context.Background(), &bridgev2.MatrixDeleteChat{
		Portal: dmPortal(), Content: &event.BeeperChatDeleteEventContent{DeleteForEveryone: true},
	}); err == nil {
		t.Fatal("delete_for_everyone must be refused")
	}
}

func TestCapabilitiesBlockReportInDMsOnly(t *testing.T) {
	b := &BlueskyClient{}
	dm := b.GetCapabilities(context.Background(), dmPortal())
	if !dm.BlockUser || !dm.ReportSpam || !dm.DeleteChat || dm.DeleteChatForEveryone {
		t.Fatalf("dm caps = %+v", dm)
	}
	group := b.GetCapabilities(context.Background(), &bridgev2.Portal{Portal: &database.Portal{RoomType: database.RoomTypeGroupDM}})
	if group.BlockUser || group.ReportSpam || !group.DeleteChat {
		t.Fatalf("group caps = %+v", group)
	}
	if dm.GetID() == group.GetID() {
		t.Fatal("DM and group features must have different IDs")
	}
}

// The capabilities version must move with the features: bridgev2 re-sends
// com.beeper.room_features to every portal at startup only when it changes.
// fetchInbox resyncs the newest 20 convos alone, so without the bump older
// DMs never offered block / report / delete chat.
func TestCapabilitiesVersionBumped(t *testing.T) {
	if _, caps := (&BlueskyConnector{}).GetBridgeInfoVersion(); caps < 2 {
		t.Fatalf("capabilities version = %d, want >= 2 (patch 0001 changed the room features)", caps)
	}
}

func TestBlockRkey(t *testing.T) {
	if k, err := blockRkey(blockURI, meDID); err != nil || k != "3blockrkey" {
		t.Fatalf("rkey %q %v", k, err)
	}
	if _, err := blockRkey("at://did:plc:other/app.bsky.graph.block/x", meDID); err == nil {
		t.Fatal("a block record in another repo must be refused")
	}
}
