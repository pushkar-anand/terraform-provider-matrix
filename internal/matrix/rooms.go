// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// StateEvent is one entry of a room's state, keyed by (Type, StateKey).
type StateEvent struct {
	Type     string          `json:"type"`
	StateKey string          `json:"state_key,omitempty"`
	Content  json.RawMessage `json:"content"`
}

// CreateRoomRequest is the body of POST /createRoom. Only the fields the
// provider exposes are modelled.
type CreateRoomRequest struct {
	Name            string          `json:"name,omitempty"`
	Topic           string          `json:"topic,omitempty"`
	Preset          string          `json:"preset,omitempty"`
	Visibility      string          `json:"visibility,omitempty"`
	RoomAliasName   string          `json:"room_alias_name,omitempty"`
	RoomVersion     string          `json:"room_version,omitempty"`
	CreationContent json.RawMessage `json:"creation_content,omitempty"`
	InitialState    []StateEvent    `json:"initial_state,omitempty"`
	Invite          []string        `json:"invite,omitempty"`
}

// CreateRoom creates a room and returns its ID.
func (c *Client) CreateRoom(ctx context.Context, req CreateRoomRequest) (string, error) {
	var resp struct {
		RoomID string `json:"room_id"`
	}

	if err := c.Do(ctx, http.MethodPost, CSAPI+"/createRoom", req, &resp); err != nil {
		return "", err
	}

	if resp.RoomID == "" {
		return "", fmt.Errorf("homeserver accepted createRoom but returned no room_id")
	}

	return resp.RoomID, nil
}

// statePath builds the state endpoint for one (eventType, stateKey) pair.
//
// The empty state key is spelled by omitting the segment: sending a trailing
// slash instead is not accepted uniformly across homeservers.
func statePath(roomID, eventType, stateKey string) string {
	path := fmt.Sprintf("%s/rooms/%s/state/%s", CSAPI, escape(roomID), escape(eventType))
	if stateKey != "" {
		path += "/" + escape(stateKey)
	}

	return path
}

// GetState returns the content of one state event.
//
// The response is the content object exactly as it was sent, with no envelope
// and no server-added fields, which is what lets the provider compare it
// against configuration without normalising first.
func (c *Client) GetState(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error) {
	var content json.RawMessage

	if err := c.Do(ctx, http.MethodGet, statePath(roomID, eventType, stateKey), nil, &content); err != nil {
		return nil, err
	}

	return content, nil
}

// SetState writes one state event and returns the resulting event ID.
func (c *Client) SetState(ctx context.Context, roomID, eventType, stateKey string, content json.RawMessage) (string, error) {
	var resp struct {
		EventID string `json:"event_id"`
	}

	if err := c.Do(ctx, http.MethodPut, statePath(roomID, eventType, stateKey), content, &resp); err != nil {
		return "", err
	}

	return resp.EventID, nil
}

// DeleteRoomRequest is the body of the Synapse admin room deletion endpoint.
type DeleteRoomRequest struct {
	// Block prevents the room being rejoined or recreated under the same ID.
	Block bool `json:"block,omitempty"`
	// Purge removes the room's history from the database.
	Purge bool `json:"purge"`
	// ForcePurge purges even when local members cannot be removed cleanly.
	ForcePurge bool `json:"force_purge,omitempty"`
}

// DeleteRoom removes a room through the Synapse admin API.
//
// This has no Client-Server API equivalent: plain Matrix can only leave a room,
// never delete it, so without the admin API a destroy would silently leave the
// room live on the server.
func (c *Client) DeleteRoom(ctx context.Context, roomID string, req DeleteRoomRequest) error {
	path := fmt.Sprintf("%s/v2/rooms/%s", AdminAPI, escape(roomID))

	return c.Do(ctx, http.MethodDelete, path, req, nil)
}

// RoomDetails is the subset of the admin room record the provider reads.
type RoomDetails struct {
	RoomID         string `json:"room_id"`
	Name           string `json:"name"`
	Topic          string `json:"topic"`
	CanonicalAlias string `json:"canonical_alias"`
	RoomVersion    string `json:"version"`
	Public         bool   `json:"public"`
}

// GetRoomDetails reads a room through the admin API, which is the only way to
// confirm a room still exists without being joined to it.
func (c *Client) GetRoomDetails(ctx context.Context, roomID string) (*RoomDetails, error) {
	var details RoomDetails

	path := fmt.Sprintf("%s/v1/rooms/%s", AdminAPI, escape(roomID))
	if err := c.Do(ctx, http.MethodGet, path, nil, &details); err != nil {
		return nil, err
	}

	return &details, nil
}

// Visibility values for the public room directory.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// GetDirectoryVisibility reports whether the room is listed in the public room
// directory. Directory listing is separate state from the room's join rules: a
// room can be invite-only yet listed, or public yet unlisted.
func (c *Client) GetDirectoryVisibility(ctx context.Context, roomID string) (string, error) {
	var resp struct {
		Visibility string `json:"visibility"`
	}

	path := fmt.Sprintf("%s/directory/list/room/%s", CSAPI, escape(roomID))
	if err := c.Do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}

	return resp.Visibility, nil
}

// SetDirectoryVisibility lists or unlists the room in the public directory.
func (c *Client) SetDirectoryVisibility(ctx context.Context, roomID, visibility string) error {
	body := struct {
		Visibility string `json:"visibility"`
	}{Visibility: visibility}

	path := fmt.Sprintf("%s/directory/list/room/%s", CSAPI, escape(roomID))

	return c.Do(ctx, http.MethodPut, path, body, nil)
}

// GetStateString reads a single string field out of one state event, returning
// "" when the event is absent.
//
// An unset name or topic is reported as M_NOT_FOUND rather than an empty event,
// so callers would otherwise have to special-case that on every read.
func (c *Client) GetStateString(ctx context.Context, roomID, eventType, field string) (string, error) {
	content, err := c.GetState(ctx, roomID, eventType, "")
	if err != nil {
		if IsNotFound(err) {
			return "", nil
		}

		return "", err
	}

	var fields map[string]any
	if err := json.Unmarshal(content, &fields); err != nil {
		return "", fmt.Errorf("decoding %s content: %w", eventType, err)
	}

	value, _ := fields[field].(string)

	return value, nil
}

// HasState reports whether a state event exists in the room.
//
// Presence is the whole signal for events like m.room.encryption, whose content
// carries no on/off flag -- the event existing is what enables the feature.
func (c *Client) HasState(ctx context.Context, roomID, eventType string) (bool, error) {
	_, err := c.GetState(ctx, roomID, eventType, "")
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
