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

// testAccPreCheck fails fast when the homeserver credentials are absent.
//
// Acceptance tests create and delete real rooms, so they need a real server;
// without this the failure surfaces as an opaque provider configuration error
// partway through a test run.
func testAccPreCheck(t *testing.T) {
	t.Helper()

	for _, key := range []string{envHomeserverURL, envAccessToken} {
		if os.Getenv(key) == "" {
			t.Fatalf("%s must be set for acceptance tests", key)
		}
	}
}
