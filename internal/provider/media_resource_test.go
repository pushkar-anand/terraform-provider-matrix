// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var mxcURIPattern = regexp.MustCompile(`^mxc://[^/]+/.+$`)

func TestAccMediaResource(t *testing.T) {
	// Written once and rewritten between steps, so the test exercises the case
	// this resource exists for: the path stays the same and the bytes change.
	source := filepath.Join(t.TempDir(), "avatar.txt")

	write := func(content string) {
		if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", source, err)
		}
	}

	digest := func(content string) string {
		sum := sha256.Sum256([]byte(content))

		return hex.EncodeToString(sum[:])
	}

	write("first")

	var firstURI string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccMediaResourceConfig(source),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("matrix_media.test", "mxc_uri", mxcURIPattern),
					resource.TestCheckResourceAttrPair(
						"matrix_media.test", "id", "matrix_media.test", "mxc_uri"),
					// Sniffed, because the configuration does not say.
					resource.TestCheckResourceAttrSet("matrix_media.test", "content_type"),
					resource.TestCheckResourceAttr("matrix_media.test", "sha256", digest("first")),
					captureAttr("matrix_media.test", "mxc_uri", &firstURI),
				),
			},
			{
				ResourceName:      "matrix_media.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Terraform cannot tell which local file the stored bytes came
				// from, so none of the content attributes round-trip on import.
				ImportStateVerifyIgnore: []string{
					"source", "content_base64", "filename", "content_type", "sha256",
				},
			},
			{
				// Same path, different bytes. The plan must notice and replace,
				// which is the whole reason ModifyPlan hashes the file.
				PreConfig: func() { write("second") },
				Config:    testAccMediaResourceConfig(source),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("matrix_media.test", "sha256", digest("second")),
					attrDiffers("matrix_media.test", "mxc_uri", &firstURI),
				),
			},
		},
	})
}

func testAccMediaResourceConfig(source string) string {
	return fmt.Sprintf(`
resource "matrix_media" "test" {
  source   = %[1]q
  filename = "avatar.txt"
}
`, source)
}

// captureAttr records an attribute value so a later step can compare against it.
func captureAttr(name, key string, into *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		res, ok := state.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}

		*into = res.Primary.Attributes[key]

		return nil
	}
}

// attrDiffers asserts a captured value has since changed, which is how a
// replacement is told apart from an update in place.
func attrDiffers(name, key string, previous *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		res, ok := state.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource %s not found in state", name)
		}

		if current := res.Primary.Attributes[key]; current == *previous {
			return fmt.Errorf("%s.%s is still %q; the media was not replaced", name, key, current)
		}

		return nil
	}
}
