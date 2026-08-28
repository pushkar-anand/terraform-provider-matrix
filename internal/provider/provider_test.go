// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories instantiates the provider during acceptance
// testing. The factory is called for each Terraform CLI command to create a
// server the CLI can connect to.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"matrix": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck skips the test when no homeserver is configured.
//
// Acceptance tests create and delete real rooms, so they need a real server.
// Skipping rather than failing keeps CI meaningful on a fork or a pull request
// that has no homeserver to point at, where a failure would say nothing about
// the change under test.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	for _, key := range []string{envHomeserverURL, envAccessToken} {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set; skipping acceptance test", key)
		}
	}
}
