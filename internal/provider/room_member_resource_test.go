// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

// satisfies decides both whether an apply acts and whether a read reports
// drift, so the asymmetry it encodes is worth pinning down: an accepted invite
// must not read as drift, while the reverse must.
func TestSatisfies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		current, target string
		want            bool
	}{
		{matrix.MembershipJoin, matrix.MembershipJoin, true},
		{matrix.MembershipInvite, matrix.MembershipInvite, true},
		{matrix.MembershipBan, matrix.MembershipBan, true},

		// The one asymmetry: Terraform can withdraw an invitation but has no way
		// to un-accept one, so accepting is not drift.
		{matrix.MembershipJoin, matrix.MembershipInvite, true},
		{matrix.MembershipInvite, matrix.MembershipJoin, false},

		{matrix.MembershipLeave, matrix.MembershipInvite, false},
		{matrix.MembershipLeave, matrix.MembershipJoin, false},
		{matrix.MembershipLeave, matrix.MembershipBan, false},
		{matrix.MembershipBan, matrix.MembershipJoin, false},
		{matrix.MembershipBan, matrix.MembershipInvite, false},
		{matrix.MembershipKnock, matrix.MembershipJoin, false},
		{matrix.MembershipJoin, matrix.MembershipBan, false},
	}

	for _, tc := range cases {
		if got := satisfies(tc.current, tc.target); got != tc.want {
			t.Errorf("satisfies(%q, %q) = %v, want %v", tc.current, tc.target, got, tc.want)
		}
	}
}

// A user ID localpart may contain '/', so the composite ID cannot be split on
// one. Round-tripping is what the import path depends on.
func TestMemberIDRoundTrips(t *testing.T) {
	t.Parallel()

	for _, userID := range []string{
		"@alice:example.org",
		"@a/b:example.org",
		"@a.b_c=d+e-f:example.org",
	} {
		roomID := "!abcdef:example.org"

		id := memberID(roomID, userID)

		gotRoom, gotUser, found := cutMemberID(id)
		if !found || gotRoom != roomID || gotUser != userID {
			t.Errorf("memberID(%q, %q) = %q, split back to (%q, %q, %v)",
				roomID, userID, id, gotRoom, gotUser, found)
		}
	}
}

func TestUserIDPattern(t *testing.T) {
	t.Parallel()

	for userID, want := range map[string]bool{
		"@alice:example.org":       true,
		"@a/b:example.org":         true,
		"@a.b_c=d+e-f:example.org": true,
		"@alice:example.org:8448":  true,
		"alice:example.org":        false,
		"@alice":                   false,
		"@Alice:example.org":       false,
		"":                         false,
	} {
		if got := userIDPattern.MatchString(userID); got != want {
			t.Errorf("userIDPattern.MatchString(%q) = %v, want %v", userID, got, want)
		}
	}
}

func TestAccRoomMemberResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoomMemberResourceConfig(matrix.MembershipInvite),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_room_member.test", "membership", matrix.MembershipInvite),
					resource.TestCheckResourceAttr("matrix_room_member.test", "current_membership", matrix.MembershipInvite),
					resource.TestCheckResourceAttrSet("matrix_room_member.test", "id"),
				),
			},
			{
				ResourceName:      "matrix_room_member.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Recorded on the event rather than readable back from it.
				ImportStateVerifyIgnore: []string{"reason"},
			},
			{
				// The forced join, which needs the Synapse admin API: the
				// Client-Server API cannot accept an invite for another account.
				Config: testAccRoomMemberResourceConfig(matrix.MembershipJoin),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_room_member.test", "membership", matrix.MembershipJoin),
					resource.TestCheckResourceAttr("matrix_room_member.test", "current_membership", matrix.MembershipJoin),
				),
			},
			{
				// Having joined, an `invite` floor must be satisfied by the join
				// rather than planning the member back out of the room.
				Config:   testAccRoomMemberResourceConfig(matrix.MembershipInvite),
				PlanOnly: true,
			},
		},
	})
}

func testAccRoomMemberResourceConfig(membership string) string {
	return `
resource "matrix_room" "test" {
  name       = "acctest room member"
  preset     = "private_chat"
  encryption = false
}

resource "matrix_user" "test" {
  localpart           = "tfacctestmember"
  display_name        = "acctest member"
  password_wo         = "correct-horse-battery-staple"
  password_wo_version = "1"
}

resource "matrix_room_member" "test" {
  room_id    = matrix_room.test.id
  user_id    = matrix_user.test.id
  membership = "` + membership + `"
  reason     = "acceptance test"
}
`
}
