// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Bool decodes a JSON boolean that may arrive as a number.
//
// Synapse stores these flags as SQLite integers and serialises several of them
// as 0/1 rather than false/true; tuwunel emits real booleans. Both are valid
// responses from the endpoints this provider calls, so both must decode.
type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = Bool(asBool)

		return nil
	}

	var asNumber float64
	if err := json.Unmarshal(data, &asNumber); err != nil {
		return fmt.Errorf("expected a boolean or a number, got %s", string(data))
	}

	*b = asNumber != 0

	return nil
}

func (b Bool) MarshalJSON() ([]byte, error) { return json.Marshal(bool(b)) }

// UserDetails is the subset of the Synapse user record the provider reads.
//
// Pointers mark the fields the homeserver reports as null when unset, so that
// "never set" stays distinguishable from "set to the empty string".
type UserDetails struct {
	Name        string  `json:"name"`
	DisplayName *string `json:"displayname"`
	AvatarURL   *string `json:"avatar_url"`
	Admin       Bool    `json:"admin"`
	Deactivated Bool    `json:"deactivated"`
	Locked      Bool    `json:"locked"`
	Suspended   Bool    `json:"suspended"`
	Erased      Bool    `json:"erased"`
	IsGuest     Bool    `json:"is_guest"`
}

// UpsertUserRequest is the body of PUT /_synapse/admin/v2/users/{user_id}.
//
// Every field is a pointer and omitted when nil, which is load-bearing twice
// over. An omitted field leaves the server-side value untouched, so partial
// updates are expressible; and an explicitly null field is rejected outright
// rather than treated as absent.
type UpsertUserRequest struct {
	Password *string `json:"password,omitempty"`
	// LogoutDevices only has an effect alongside Password. The server defaults
	// it to true, so a password change ends every existing session unless this
	// says otherwise.
	LogoutDevices *bool   `json:"logout_devices,omitempty"`
	DisplayName   *string `json:"displayname,omitempty"`
	AvatarURL     *string `json:"avatar_url,omitempty"`
	Admin         *bool   `json:"admin,omitempty"`
	Deactivated   *bool   `json:"deactivated,omitempty"`
	Locked        *bool   `json:"locked,omitempty"`
}

// userPath builds the admin v2 endpoint for one user.
func userPath(userID string) string {
	return fmt.Sprintf("%s/v2/users/%s", AdminAPI, escape(userID))
}

// UpsertUser creates a user or modifies an existing one, and returns the
// resulting record.
//
// The endpoint is a genuine upsert: it creates when the user is absent and
// patches when it is present, with no separate create call and no error on the
// second apply. That is what makes it usable as a Terraform resource at all --
// the registration endpoints error on an existing user instead.
func (c *Client) UpsertUser(ctx context.Context, userID string, req UpsertUserRequest) (*UserDetails, error) {
	var details UserDetails

	if err := c.Do(ctx, http.MethodPut, userPath(userID), req, &details); err != nil {
		return nil, err
	}

	return &details, nil
}

// GetUser reads a user through the admin API.
func (c *Client) GetUser(ctx context.Context, userID string) (*UserDetails, error) {
	var details UserDetails

	if err := c.Do(ctx, http.MethodGet, userPath(userID), nil, &details); err != nil {
		return nil, err
	}

	return &details, nil
}

// DeactivateUser deactivates an account, optionally erasing its data.
//
// This is as close to deletion as Matrix gets. The user ID stays permanently
// claimed on the homeserver: a deactivated account still exists, so the
// localpart can never be registered again.
func (c *Client) DeactivateUser(ctx context.Context, userID string, erase bool) error {
	body := struct {
		Erase bool `json:"erase"`
	}{Erase: erase}

	path := fmt.Sprintf("%s/v1/deactivate/%s", AdminAPI, escape(userID))

	return c.Do(ctx, http.MethodPost, path, body, nil)
}

// SetIdentity records the user ID the token authenticates as, as returned by
// Whoami.
func (c *Client) SetIdentity(userID string) { c.userID = userID }

// UserID returns the user ID the provider acts as.
func (c *Client) UserID() string { return c.userID }

// ServerName returns the homeserver's name, taken from the authenticated user
// ID rather than from the URL.
//
// The two differ routinely: this server answers on matrix.lab.pushkar.dev but
// names itself lab.pushkar.dev, and it is the latter that user IDs are built
// from. Deriving it from the URL would produce MXIDs the server rejects.
func (c *Client) ServerName() string {
	_, serverName, found := strings.Cut(c.userID, ":")
	if !found {
		return ""
	}

	return serverName
}

// UserIDFor builds the full user ID for a localpart on this homeserver.
func (c *Client) UserIDFor(localpart string) string {
	return "@" + localpart + ":" + c.ServerName()
}
