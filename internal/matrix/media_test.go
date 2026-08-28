// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import "testing"

func TestParseMXC(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		server, media, err := ParseMXC("mxc://lab.example.org/AbCdEf123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if server != "lab.example.org" || media != "AbCdEf123" {
			t.Errorf("got (%q, %q), want (lab.example.org, AbCdEf123)", server, media)
		}
	})

	// An https:// URL is the mistake worth catching: it is what someone reaches
	// for when setting an avatar, and the homeserver rejects it far less clearly.
	for name, uri := range map[string]string{
		"http url":     "https://example.org/avatar.png",
		"no media id":  "mxc://example.org",
		"empty server": "mxc:///AbCdEf123",
		"empty":        "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, _, err := ParseMXC(uri); err == nil {
				t.Errorf("ParseMXC(%q) succeeded, want an error", uri)
			}
		})
	}
}
