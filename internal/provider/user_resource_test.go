// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccUserResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccUserResourceConfig("acctest user", "1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_user.test", "localpart", "tfacctest"),
					resource.TestCheckResourceAttr("matrix_user.test", "display_name", "acctest user"),
					resource.TestCheckResourceAttr("matrix_user.test", "admin", "false"),
					resource.TestCheckResourceAttr("matrix_user.test", "deactivated", "false"),
					resource.TestCheckResourceAttr("matrix_user.test", "locked", "false"),
					// Built from the localpart and the server name the token carries.
					resource.TestCheckResourceAttrSet("matrix_user.test", "id"),
					// The password must never reach state, which is the entire
					// point of declaring it write-only.
					resource.TestCheckNoResourceAttr("matrix_user.test", "password_wo"),
				),
			},
			{
				ResourceName:      "matrix_user.test",
				ImportState:       true,
				ImportStateVerify: true,
				// None of these exist server-side: the password is write-only and
				// the rest describe what Terraform should do, not what the account is.
				ImportStateVerifyIgnore: []string{
					"password_wo",
					"password_wo_version",
					"logout_devices_on_password_change",
					"erase_on_destroy",
				},
			},
			{
				// Renaming and rotating the password at once: the version bump is
				// what makes the new password get sent.
				Config: testAccUserResourceConfig("acctest user renamed", "2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_user.test", "display_name", "acctest user renamed"),
					resource.TestCheckResourceAttr("matrix_user.test", "password_wo_version", "2"),
				),
			},
		},
	})
}

func testAccUserResourceConfig(displayName, passwordVersion string) string {
	return fmt.Sprintf(`
resource "matrix_user" "test" {
  localpart    = "tfacctest"
  display_name = %[1]q

  password_wo         = "acctest-password-%[2]s"
  password_wo_version = %[2]q
}
`, displayName, passwordVersion)
}
