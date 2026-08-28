// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

var (
	_ resource.Resource                = &MediaResource{}
	_ resource.ResourceWithConfigure   = &MediaResource{}
	_ resource.ResourceWithImportState = &MediaResource{}
	_ resource.ResourceWithModifyPlan  = &MediaResource{}
)

// MediaResource manages one file in the homeserver's content repository.
type MediaResource struct {
	client *matrix.Client
}

// MediaResourceModel describes the media resource data model.
type MediaResourceModel struct {
	ID            types.String `tfsdk:"id"`
	MXCURI        types.String `tfsdk:"mxc_uri"`
	Source        types.String `tfsdk:"source"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	Filename      types.String `tfsdk:"filename"`
	ContentType   types.String `tfsdk:"content_type"`
	SHA256        types.String `tfsdk:"sha256"`
}

func NewMediaResource() resource.Resource {
	return &MediaResource{}
}

func (r *MediaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_media"
}

func (r *MediaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// Every configurable attribute forces replacement because uploaded media is
	// immutable: the homeserver assigns a random media ID and the bytes behind it
	// can never be rewritten. Changing anything means a new upload and a new URI,
	// so this resource is only ever created and destroyed, never updated.
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A file in the homeserver's content repository, uploaded so its " +
			"`mxc://` URI can be used elsewhere — most usefully as a `matrix_user` avatar, since " +
			"Matrix profile pictures are content-repository references rather than ordinary URLs." +
			"\n\n~> **Uploads are immutable.** The homeserver assigns a random media ID and the " +
			"bytes behind it can never be rewritten, so any change here uploads a new file under a " +
			"new URI and deletes the old one. Deleting media that something still refers to leaves " +
			"a broken reference — the homeserver does not track who points at what.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The `mxc://` URI the homeserver assigned. Identical to `mxc_uri`.",
				Computed:            true,
			},
			"mxc_uri": schema.StringAttribute{
				MarkdownDescription: "The `mxc://` URI the homeserver assigned, for example " +
					"`mxc://example.org/AbCdEf123`. This is the value other resources reference.",
				Computed: true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "Path to a local file to upload. Exactly one of `source` or " +
					"`content_base64` is required. The file's contents are hashed on every plan, so " +
					"editing the file in place is detected even though the path has not changed.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(path.MatchRoot("content_base64")),
				},
				PlanModifiers: replace,
			},
			"content_base64": schema.StringAttribute{
				MarkdownDescription: "Base64-encoded file contents, for media generated in the " +
					"configuration rather than read from disk. Exactly one of `source` or " +
					"`content_base64` is required.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"filename": schema.StringAttribute{
				MarkdownDescription: "Filename recorded with the upload, which clients use when " +
					"offering the file for download. Defaults to none.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"content_type": schema.StringAttribute{
				MarkdownDescription: "MIME type recorded with the upload, for example `image/png`. " +
					"Detected from the file's contents when omitted.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: replace,
			},
			"sha256": schema.StringAttribute{
				MarkdownDescription: "Hex-encoded SHA-256 of the uploaded bytes. Computed during " +
					"planning for `source`, which is what lets an edit to the file on disk force a " +
					"new upload.",
				Computed: true,
			},
		},
	}
}

func (r *MediaResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*matrix.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *matrix.Client, got %T. This is a bug in the provider.", req.ProviderData),
		)

		return
	}

	r.client = client
}

// ModifyPlan hashes the source file during planning so that editing it in place
// is noticed.
//
// Without this the resource would only ever change when the configuration did,
// and a new logo written to the same path would never be uploaded — the plan
// would be empty and the avatar would silently stay stale.
func (r *MediaResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan MediaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Inline content is part of the configuration and diffs on its own; only a
	// file on disk can change without Terraform being told.
	if !isSet(plan.Source) {
		return
	}

	content, err := os.ReadFile(plan.Source.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("source"), "Cannot read source file", err.Error())

		return
	}

	digest := hashBytes(content)
	plan.SHA256 = types.StringValue(digest)

	// Detect the type here as well as at apply, from the same bytes. Left to
	// apply alone, an unconfigured content_type would be planned as whatever the
	// previous upload had -- so swapping a PNG for a JPEG at the same path would
	// plan "image/png" and apply "image/jpeg", and Terraform would reject the
	// result as inconsistent with the plan.
	var config MediaResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if config.ContentType.IsNull() {
		plan.ContentType = types.StringValue(http.DetectContentType(content))
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)

	if req.State.Raw.IsNull() {
		return
	}

	var state MediaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if !state.SHA256.IsNull() && state.SHA256.ValueString() != digest {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("sha256"))
	}
}

func (r *MediaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MediaResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	content, diags := readContent(plan)
	resp.Diagnostics.Append(diags...)

	if resp.Diagnostics.HasError() {
		return
	}

	contentType := plan.ContentType.ValueString()
	if !isSet(plan.ContentType) {
		// Sniffing beats defaulting to application/octet-stream: clients decide
		// whether something is displayable from this, so a wrong value here is the
		// difference between an avatar rendering and not.
		contentType = http.DetectContentType(content)
	}

	uri, err := r.client.UploadMedia(ctx, plan.Filename.ValueString(), contentType, content)
	if err != nil {
		resp.Diagnostics.AddError("Cannot upload media", err.Error())

		return
	}

	plan.ID = types.StringValue(uri)
	plan.MXCURI = types.StringValue(uri)
	plan.ContentType = types.StringValue(contentType)
	plan.SHA256 = types.StringValue(hashBytes(content))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MediaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MediaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := r.client.MediaExists(ctx, state.ID.ValueString())
	if err != nil {
		if matrix.IsUnrecognized(err) {
			// Without the admin API there is no way to ask whether media still
			// exists short of downloading it, so leave state alone rather than
			// deleting a resource that is probably fine.
			return
		}

		resp.Diagnostics.AddError("Cannot read media", err.Error())

		return
	}

	if !exists {
		resp.State.RemoveResource(ctx)

		return
	}

	// Nothing else can have changed: the bytes behind an mxc:// URI are fixed for
	// its lifetime, so there is no drift to reconcile.
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update cannot happen: every configurable attribute forces replacement.
func (r *MediaResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Media cannot be updated",
		"Uploaded media is immutable, so every change replaces it. Reaching this point is a bug "+
			"in the provider.",
	)
}

func (r *MediaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state MediaResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMedia(ctx, state.ID.ValueString())
	if err == nil || matrix.IsNotFound(err) {
		return
	}

	if matrix.IsUnrecognized(err) {
		resp.Diagnostics.AddError(
			"Homeserver cannot delete media",
			"The homeserver does not serve the Synapse admin media endpoint. The file is still "+
				"stored on the server and must be removed by hand.",
		)

		return
	}

	resp.Diagnostics.AddError("Cannot delete media", err.Error())
}

func (r *MediaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, _, err := matrix.ParseMXC(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid media URI", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("mxc_uri"), req.ID)...)
}

// readContent returns the bytes to upload from whichever of the two sources the
// configuration used.
func readContent(plan MediaResourceModel) ([]byte, diag.Diagnostics) {
	var diags diag.Diagnostics

	switch {
	case isSet(plan.Source):
		content, err := os.ReadFile(plan.Source.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("source"), "Cannot read source file", err.Error())

			return nil, diags
		}

		return content, diags

	case isSet(plan.ContentBase64):
		content, err := base64.StdEncoding.DecodeString(plan.ContentBase64.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("content_base64"), "Cannot decode content", err.Error())

			return nil, diags
		}

		return content, diags
	}

	diags.AddError("No content to upload", "Set either source or content_base64.")

	return nil, diags
}

func hashBytes(content []byte) string {
	digest := sha256.Sum256(content)

	return hex.EncodeToString(digest[:])
}
