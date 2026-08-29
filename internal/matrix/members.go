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

// Membership states of the m.room.member event.
const (
	MembershipInvite = "invite"
	MembershipJoin   = "join"
	MembershipLeave  = "leave"
	MembershipBan    = "ban"
	MembershipKnock  = "knock"
)

// EventRoomMember is the state event that carries room membership.
const EventRoomMember = "m.room.member"

// membershipRequest is the body shared by the invite, kick, ban and unban
// endpoints, all of which take a target and an optional reason.
type membershipRequest struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason,omitempty"`
}

// membershipAction issues one of the Client-Server membership endpoints.
func (c *Client) membershipAction(ctx context.Context, roomID, action, userID, reason string) error {
	path := fmt.Sprintf("%s/rooms/%s/%s", CSAPI, escape(roomID), action)

	return c.Do(ctx, http.MethodPost, path, membershipRequest{UserID: userID, Reason: reason}, nil)
}

// InviteUser invites a user to a room. The invitee decides whether to accept,
// so this leaves membership at "invite" rather than "join".
func (c *Client) InviteUser(ctx context.Context, roomID, userID, reason string) error {
	return c.membershipAction(ctx, roomID, "invite", userID, reason)
}

// KickUser sets a user's membership back to "leave", which both removes a
// joined member and withdraws an invite that was never accepted.
func (c *Client) KickUser(ctx context.Context, roomID, userID, reason string) error {
	return c.membershipAction(ctx, roomID, "kick", userID, reason)
}

// BanUser bans a user, removing them from the room if they were in it.
func (c *Client) BanUser(ctx context.Context, roomID, userID, reason string) error {
	return c.membershipAction(ctx, roomID, "ban", userID, reason)
}

// UnbanUser lifts a ban, leaving membership at "leave".
func (c *Client) UnbanUser(ctx context.Context, roomID, userID, reason string) error {
	return c.membershipAction(ctx, roomID, "unban", userID, reason)
}

// AdminJoinUser forces a local user into a room through the Synapse admin API.
//
// There is no Client-Server equivalent and there cannot be: the Matrix auth
// rules require a "join" membership event to be sent by the joining user
// themselves, so an administrator can invite an account but never accept on its
// behalf. The endpoint only works for local users, and the account the provider
// authenticates as must itself be in the room with permission to invite.
func (c *Client) AdminJoinUser(ctx context.Context, roomID, userID string) error {
	body := struct {
		UserID string `json:"user_id"`
	}{UserID: userID}

	path := fmt.Sprintf("%s/v1/join/%s", AdminAPI, escape(roomID))

	return c.Do(ctx, http.MethodPost, path, body, nil)
}

// GetMembership reports a user's membership in a room, returning "leave" when
// the room holds no member event for them.
//
// A user who was never in the room has no m.room.member event at all, which the
// server reports as M_NOT_FOUND. That is the same situation as having left, and
// Matrix itself treats the two identically, so both collapse to "leave".
func (c *Client) GetMembership(ctx context.Context, roomID, userID string) (string, error) {
	content, err := c.GetState(ctx, roomID, EventRoomMember, userID)
	if err != nil {
		if IsNotFound(err) {
			return MembershipLeave, nil
		}

		return "", err
	}

	var member struct {
		Membership string `json:"membership"`
	}

	if err := json.Unmarshal(content, &member); err != nil {
		return "", fmt.Errorf("decoding %s content: %w", EventRoomMember, err)
	}

	if member.Membership == "" {
		return MembershipLeave, nil
	}

	return member.Membership, nil
}

// IsLocalUser reports whether a user ID belongs to this homeserver, which is
// what the admin join endpoint is limited to.
func (c *Client) IsLocalUser(userID string) bool {
	_, serverName, found := strings.Cut(userID, ":")

	return found && serverName == c.ServerName()
}
