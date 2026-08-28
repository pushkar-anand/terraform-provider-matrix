// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

// Package matrix is a minimal client for the Matrix Client-Server API and the
// Synapse admin API, covering only what the provider needs.
//
// Both Synapse and tuwunel serve these APIs, so nothing here may depend on a
// particular homeserver implementation.
package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// CSAPI is the versioned Client-Server API prefix, used for rooms and state.
	CSAPI = "/_matrix/client/v3"
	// AdminAPI is the Synapse admin API prefix, used for users and for room
	// deletion, which the Client-Server API cannot do at all.
	AdminAPI = "/_synapse/admin"
)

// Client talks to one homeserver as one authenticated user.
type Client struct {
	base      *url.URL
	token     string
	userAgent string
	hc        *http.Client
}

// NewClient returns a client for the homeserver at rawURL authenticating with
// the given access token.
func NewClient(rawURL, token, userAgent string, timeout time.Duration) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parsing homeserver url: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("homeserver url must be http or https, got %q", base.Scheme)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("homeserver url %q has no host", rawURL)
	}

	return &Client{
		base:      base,
		token:     token,
		userAgent: userAgent,
		hc:        &http.Client{Timeout: timeout},
	}, nil
}

// APIError is a Matrix error response. The Client-Server API and the Synapse
// admin API share this shape, so one type covers both.
type APIError struct {
	// Status is the HTTP status, kept because some failures arrive with an
	// empty or non-JSON body that leaves Code unset.
	Status  int
	Code    string `json:"errcode"`
	Message string `json:"error"`
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Code, e.Message, e.Status)
	case e.Code != "":
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.Status)
	default:
		return fmt.Sprintf("unexpected response (HTTP %d)", e.Status)
	}
}

// IsNotFound reports whether err means the object does not exist, which is how
// Read tells "removed outside Terraform" apart from a real failure.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	return apiErr.Status == http.StatusNotFound || apiErr.Code == "M_NOT_FOUND"
}

// IsUnrecognized reports whether the homeserver does not implement the
// endpoint, which is how a server without the Synapse admin API answers.
func IsUnrecognized(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	return apiErr.Code == "M_UNRECOGNIZED"
}

// Do issues a request against path, encoding in as JSON when non-nil and
// decoding the response into out when non-nil.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader

	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}

		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, body)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode}
		// A non-JSON body is normal for proxy-generated errors; the status alone
		// still identifies the failure.
		_ = json.Unmarshal(payload, apiErr)

		return apiErr
	}

	if out == nil || len(payload) == 0 {
		return nil
	}

	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
	}

	return nil
}

// Whoami returns the user ID the configured token authenticates as. It is the
// cheapest call that proves both reachability and a valid token.
func (c *Client) Whoami(ctx context.Context) (string, error) {
	var resp struct {
		UserID string `json:"user_id"`
	}

	if err := c.Do(ctx, http.MethodGet, CSAPI+"/account/whoami", nil, &resp); err != nil {
		return "", err
	}

	return resp.UserID, nil
}

// escape encodes one path segment. Room IDs and user IDs contain '!', '@' and
// ':', which must not reach the server raw.
func escape(segment string) string { return url.PathEscape(segment) }
