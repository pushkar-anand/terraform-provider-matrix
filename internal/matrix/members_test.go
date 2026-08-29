// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns a client pointed at a stub homeserver.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "token", "test", 5*time.Second)
	if err != nil {
		t.Fatalf("building client: %v", err)
	}

	return client
}

// A user who was never in a room has no member event at all, which the server
// reports as a 404. That is indistinguishable from having left, and the caller
// must not have to special-case it.
func TestGetMembershipTreatsMissingEventAsLeave(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"Event not found."}`))
	}))

	got, err := client.GetMembership(t.Context(), "!r:example.org", "@a:example.org")
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}

	if got != MembershipLeave {
		t.Errorf("got %q, want %q", got, MembershipLeave)
	}
}

func TestGetMembershipReadsMemberEvent(t *testing.T) {
	t.Parallel()

	var gotPath string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"membership":"join","displayname":"A"}`))
	}))

	got, err := client.GetMembership(t.Context(), "!r:example.org", "@a:example.org")
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}

	if got != MembershipJoin {
		t.Errorf("got membership %q, want %q", got, MembershipJoin)
	}

	// The '!', '@' and ':' in the IDs must not reach the server raw.
	want := "/_matrix/client/v3/rooms/%21r:example.org/state/m.room.member/@a:example.org"
	if gotPath != want {
		t.Errorf("got path %q, want %q", gotPath, want)
	}
}

// Every membership endpoint takes the target in the body rather than the path,
// and omits the reason entirely when there is none.
func TestMembershipActionsPostUserID(t *testing.T) {
	t.Parallel()

	for action, call := range map[string]func(context.Context, *Client) error{
		"invite": func(ctx context.Context, c *Client) error {
			return c.InviteUser(ctx, "!r:example.org", "@a:example.org", "")
		},
		"kick": func(ctx context.Context, c *Client) error {
			return c.KickUser(ctx, "!r:example.org", "@a:example.org", "")
		},
		"ban": func(ctx context.Context, c *Client) error {
			return c.BanUser(ctx, "!r:example.org", "@a:example.org", "")
		},
		"unban": func(ctx context.Context, c *Client) error {
			return c.UnbanUser(ctx, "!r:example.org", "@a:example.org", "")
		},
	} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotBody, gotMethod string

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.EscapedPath()

				body := make([]byte, r.ContentLength)
				_, _ = r.Body.Read(body)
				gotBody = string(body)

				_, _ = w.Write([]byte(`{}`))
			}))

			if err := call(t.Context(), client); err != nil {
				t.Fatalf("%s: %v", action, err)
			}

			if gotMethod != http.MethodPost {
				t.Errorf("got method %q, want POST", gotMethod)
			}

			wantPath := "/_matrix/client/v3/rooms/%21r:example.org/" + action
			if gotPath != wantPath {
				t.Errorf("got path %q, want %q", gotPath, wantPath)
			}

			if want := `{"user_id":"@a:example.org"}`; gotBody != want {
				t.Errorf("got body %q, want %q", gotBody, want)
			}
		})
	}
}

func TestAdminJoinUserTargetsTheAdminAPI(t *testing.T) {
	t.Parallel()

	var gotPath, gotBody string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()

		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = string(body)

		_, _ = w.Write([]byte(`{"room_id":"!r:example.org"}`))
	}))

	if err := client.AdminJoinUser(t.Context(), "!r:example.org", "@a:example.org"); err != nil {
		t.Fatalf("AdminJoinUser: %v", err)
	}

	if want := "/_synapse/admin/v1/join/%21r:example.org"; gotPath != want {
		t.Errorf("got path %q, want %q", gotPath, want)
	}

	if want := `{"user_id":"@a:example.org"}`; gotBody != want {
		t.Errorf("got body %q, want %q", gotBody, want)
	}
}

// The admin join endpoint only works for local accounts, so the caller has to
// be able to tell them apart -- by the server name the token carries, not the
// URL the client was built with.
func TestIsLocalUserComparesAgainstTheTokensServerName(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	client.SetIdentity("@admin:example.org")

	for userID, want := range map[string]bool{
		"@a:example.org":       true,
		"@a:other.example.org": false,
		"@a:example.com":       false,
		"not-a-user-id":        false,
	} {
		if got := client.IsLocalUser(userID); got != want {
			t.Errorf("IsLocalUser(%q) = %v, want %v", userID, got, want)
		}
	}
}

// The reason is carried on the event, so it has to survive encoding when set.
func TestMembershipRequestCarriesReason(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(membershipRequest{UserID: "@a:example.org", Reason: "spam"})
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	if want := `{"user_id":"@a:example.org","reason":"spam"}`; string(encoded) != want {
		t.Errorf("got %s, want %s", encoded, want)
	}
}

// A room with no join rules event admits by invitation, per the specification,
// so an absent event must not read as "anyone may join".
func TestGetJoinRuleDefaultsToInvite(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errcode":"M_NOT_FOUND","error":"Event not found."}`))
	}))

	got, err := client.GetJoinRule(t.Context(), "!r:example.org")
	if err != nil {
		t.Fatalf("GetJoinRule: %v", err)
	}

	if got != JoinRuleInvite {
		t.Errorf("got %q, want %q", got, JoinRuleInvite)
	}
}

func TestGetJoinRuleReadsTheEvent(t *testing.T) {
	t.Parallel()

	for _, rule := range []string{JoinRulePublic, JoinRuleInvite, "knock", "restricted"} {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"join_rule":"` + rule + `"}`))
		}))

		got, err := client.GetJoinRule(t.Context(), "!r:example.org")
		if err != nil {
			t.Fatalf("GetJoinRule: %v", err)
		}

		if got != rule {
			t.Errorf("got %q, want %q", got, rule)
		}
	}
}
