// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccRoomResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoomResourceConfig("acctest room", "first topic"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_room.test", "name", "acctest room"),
					resource.TestCheckResourceAttr("matrix_room.test", "topic", "first topic"),
					resource.TestCheckResourceAttr("matrix_room.test", "visibility", "private"),
					// Encrypted unless asked otherwise.
					resource.TestCheckResourceAttr("matrix_room.test", "encryption", "true"),
					// The homeserver assigns both, so they only appear after apply.
					resource.TestCheckResourceAttrSet("matrix_room.test", "id"),
					resource.TestCheckResourceAttrSet("matrix_room.test", "room_version"),
				),
			},
			{
				ResourceName:      "matrix_room.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Destroy behaviour is provider-side only and has no server-side
				// counterpart to import, so it cannot round-trip.
				ImportStateVerifyIgnore: []string{"block_on_destroy", "purge_on_destroy"},
			},
			{
				Config: testAccRoomResourceConfig("acctest room renamed", "second topic"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_room.test", "name", "acctest room renamed"),
					resource.TestCheckResourceAttr("matrix_room.test", "topic", "second topic"),
				),
			},
		},
	})
}

func testAccRoomResourceConfig(name, topic string) string {
	return fmt.Sprintf(`
resource "matrix_room" "test" {
  name       = %[1]q
  topic      = %[2]q
  preset     = "private_chat"
  visibility = "private"
}
`, name, topic)
}
