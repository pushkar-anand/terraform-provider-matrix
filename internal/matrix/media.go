// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package matrix

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MediaAPI is the unauthenticated-era media prefix, which is still where upload
// lives. Downloads moved under the client API in Matrix 1.11, but
// POST /upload did not move with them.
const MediaAPI = "/_matrix/media/v3"

// UploadMedia stores a file on the homeserver and returns its mxc:// URI.
//
// Uploads are content, not configuration: the server assigns a random media ID
// and the bytes behind it can never be changed. Re-uploading the same file
// yields a different URI rather than reusing the old one.
func (c *Client) UploadMedia(ctx context.Context, filename, contentType string, content []byte) (string, error) {
	path := MediaAPI + "/upload"
	if filename != "" {
		path += "?filename=" + url.QueryEscape(filename)
	}

	var resp struct {
		ContentURI string `json:"content_uri"`
	}

	if err := c.doRaw(ctx, http.MethodPost, path, contentType, content, &resp); err != nil {
		return "", err
	}

	if resp.ContentURI == "" {
		return "", fmt.Errorf("homeserver accepted the upload but returned no content_uri")
	}

	return resp.ContentURI, nil
}

// ParseMXC splits an mxc:// URI into its server name and media ID.
func ParseMXC(uri string) (serverName, mediaID string, err error) {
	rest, found := strings.CutPrefix(uri, "mxc://")
	if !found {
		return "", "", fmt.Errorf("%q is not an mxc:// URI", uri)
	}

	serverName, mediaID, found = strings.Cut(rest, "/")
	if !found || serverName == "" || mediaID == "" {
		return "", "", fmt.Errorf("%q is not of the form mxc://server/media-id", uri)
	}

	return serverName, mediaID, nil
}

// MediaExists reports whether the homeserver still holds the media behind an
// mxc:// URI.
//
// This reads the admin metadata record rather than downloading the file, which
// would otherwise mean pulling the whole blob back on every refresh just to
// learn that it is still there.
func (c *Client) MediaExists(ctx context.Context, uri string) (bool, error) {
	serverName, mediaID, err := ParseMXC(uri)
	if err != nil {
		return false, err
	}

	path := fmt.Sprintf("%s/v1/media/%s/%s", AdminAPI, escape(serverName), escape(mediaID))

	if err := c.Do(ctx, http.MethodGet, path, nil, nil); err != nil {
		if IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// DeleteMedia removes an uploaded file through the Synapse admin API.
func (c *Client) DeleteMedia(ctx context.Context, uri string) error {
	serverName, mediaID, err := ParseMXC(uri)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s/v1/media/%s/%s", AdminAPI, escape(serverName), escape(mediaID))

	return c.Do(ctx, http.MethodDelete, path, nil, nil)
}
