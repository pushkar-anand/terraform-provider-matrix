// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import (
	"encoding/json"
	"testing"
)

// Synapse serialises several user flags as SQLite integers while tuwunel emits
// real booleans, and the provider has to read both.
func TestUserDetailsDecodesBoolsAndNumbers(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]string{
		"booleans": `{"name":"@a:x","admin":true,"deactivated":false,"locked":true}`,
		"numbers":  `{"name":"@a:x","admin":1,"deactivated":0,"locked":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var details UserDetails
			if err := json.Unmarshal([]byte(payload), &details); err != nil {
				t.Fatalf("decoding %s: %v", payload, err)
			}

			if !details.Admin || details.Deactivated || !details.Locked {
				t.Errorf("got admin=%v deactivated=%v locked=%v, want true/false/true",
					details.Admin, details.Deactivated, details.Locked)
			}
		})
	}
}

// An absent display name and one cleared to "" mean different things to the
// resource, so decoding must keep them apart.
func TestUserDetailsDistinguishesNullFromEmpty(t *testing.T) {
	t.Parallel()

	var absent UserDetails
	if err := json.Unmarshal([]byte(`{"name":"@a:x","displayname":null}`), &absent); err != nil {
		t.Fatalf("decoding null displayname: %v", err)
	}

	if absent.DisplayName != nil {
		t.Errorf("null displayname decoded to %q, want nil", *absent.DisplayName)
	}

	var empty UserDetails
	if err := json.Unmarshal([]byte(`{"name":"@a:x","displayname":""}`), &empty); err != nil {
		t.Fatalf("decoding empty displayname: %v", err)
	}

	if empty.DisplayName == nil || *empty.DisplayName != "" {
		t.Errorf("empty displayname decoded to %v, want a pointer to \"\"", empty.DisplayName)
	}
}

// The homeserver rejects a body that spells an unset field as null, so every
// field the caller did not set must be absent from the JSON entirely.
func TestUpsertUserRequestOmitsUnsetFields(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(UpsertUserRequest{Admin: ptrTo(false)})
	if err != nil {
		t.Fatalf("encoding request: %v", err)
	}

	if got, want := string(encoded), `{"admin":false}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// A field set to its zero value must still be sent: "" is how a display name
	// is cleared, and false is a meaningful value for every flag here.
	encoded, err = json.Marshal(UpsertUserRequest{DisplayName: ptrTo("")})
	if err != nil {
		t.Fatalf("encoding request: %v", err)
	}

	if got, want := string(encoded), `{"displayname":""}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestServerNameComesFromTheUserID(t *testing.T) {
	t.Parallel()

	// The URL and the server name differ on purpose: this is the case that makes
	// deriving the name from the URL wrong.
	client, err := NewClient("https://matrix.lab.example.org", "token", "test", 0)
	if err != nil {
		t.Fatalf("building client: %v", err)
	}

	client.SetIdentity("@pushkar:lab.example.org")

	if got, want := client.ServerName(), "lab.example.org"; got != want {
		t.Errorf("ServerName() = %q, want %q", got, want)
	}

	if got, want := client.UserIDFor("alice"), "@alice:lab.example.org"; got != want {
		t.Errorf("UserIDFor() = %q, want %q", got, want)
	}
}

func ptrTo[T any](value T) *T { return &value }
