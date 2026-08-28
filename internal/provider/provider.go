// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

const (
	envHomeserverURL = "MATRIX_HOMESERVER_URL"
	envAccessToken   = "MATRIX_ACCESS_TOKEN"

	defaultTimeoutSeconds = 30
)

// Ensure MatrixProvider satisfies the provider interface.
var _ provider.Provider = &MatrixProvider{}

// MatrixProvider manages rooms and users on a Matrix homeserver.
type MatrixProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and run locally, and "test" during acceptance testing.
	version string
}

// MatrixProviderModel describes the provider configuration.
type MatrixProviderModel struct {
	HomeserverURL  types.String `tfsdk:"homeserver_url"`
	AccessToken    types.String `tfsdk:"access_token"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
}

func (p *MatrixProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "matrix"
	resp.Version = p.version
}

func (p *MatrixProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages rooms and users on a Matrix homeserver, using the " +
			"Client-Server API for rooms and the Synapse admin API for users and room deletion. " +
			"Works against any homeserver serving both, including Synapse and tuwunel.",
		Attributes: map[string]schema.Attribute{
			"homeserver_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the homeserver, for example `https://matrix.example.org`. " +
					"May also be set with the `" + envHomeserverURL + "` environment variable.",
				Optional: true,
			},
			"access_token": schema.StringAttribute{
				MarkdownDescription: "Access token of the user the provider acts as. Managing users, " +
					"or deleting rooms, requires this to be a server administrator. " +
					"May also be set with the `" + envAccessToken + "` environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "Per-request timeout in seconds. Defaults to 30.",
				Optional:            true,
			},
		},
	}
}

func (p *MatrixProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config MatrixProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown value here means it comes from another resource that has not
	// been applied yet; the provider cannot be configured until it resolves.
	if config.HomeserverURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("homeserver_url"),
			"Unknown homeserver URL",
			"The homeserver URL is not known at configuration time. Apply the resource that "+
				"produces it first, or set it statically.",
		)
	}

	if config.AccessToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("access_token"),
			"Unknown access token",
			"The access token is not known at configuration time. Apply the resource that "+
				"produces it first, or set it statically.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	homeserverURL := os.Getenv(envHomeserverURL)
	if !config.HomeserverURL.IsNull() {
		homeserverURL = config.HomeserverURL.ValueString()
	}

	accessToken := os.Getenv(envAccessToken)
	if !config.AccessToken.IsNull() {
		accessToken = config.AccessToken.ValueString()
	}

	if homeserverURL == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("homeserver_url"),
			"Missing homeserver URL",
			"Set the `homeserver_url` attribute or the "+envHomeserverURL+" environment variable.",
		)
	}

	if accessToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("access_token"),
			"Missing access token",
			"Set the `access_token` attribute or the "+envAccessToken+" environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	timeout := time.Duration(defaultTimeoutSeconds) * time.Second
	if !config.TimeoutSeconds.IsNull() {
		timeout = time.Duration(config.TimeoutSeconds.ValueInt64()) * time.Second
	}

	client, err := matrix.NewClient(homeserverURL, accessToken, "terraform-provider-matrix/"+p.version, timeout)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Matrix client configuration", err.Error())

		return
	}

	// Fail here rather than inside the first resource: one round trip turns an
	// unreachable server or a dead token into a clear provider-level error.
	userID, err := client.Whoami(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Cannot authenticate against the homeserver",
			"Verifying the access token with /account/whoami failed: "+err.Error(),
		)

		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client

	_ = userID
}

func (p *MatrixProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRoomResource,
	}
}

func (p *MatrixProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MatrixProvider{
			version: version,
		}
	}
}
