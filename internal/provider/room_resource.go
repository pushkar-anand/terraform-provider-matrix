// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

const (
	eventRoomName       = "m.room.name"
	eventRoomTopic      = "m.room.topic"
	eventRoomEncryption = "m.room.encryption"

	// The only algorithm Matrix defines for room encryption.
	megolmV1 = "m.megolm.v1.aes-sha2"
)

var (
	_ resource.Resource                = &RoomResource{}
	_ resource.ResourceWithConfigure   = &RoomResource{}
	_ resource.ResourceWithImportState = &RoomResource{}
)

// RoomResource manages a Matrix room.
type RoomResource struct {
	client *matrix.Client
}

// RoomResourceModel describes the room resource data model.
type RoomResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Topic          types.String `tfsdk:"topic"`
	Preset         types.String `tfsdk:"preset"`
	Visibility     types.String `tfsdk:"visibility"`
	AliasLocalpart types.String `tfsdk:"alias_localpart"`
	RoomVersion    types.String `tfsdk:"room_version"`
	Encryption     types.Bool   `tfsdk:"encryption"`
	Block          types.Bool   `tfsdk:"block_on_destroy"`
	Purge          types.Bool   `tfsdk:"purge_on_destroy"`
}

func NewRoomResource() resource.Resource {
	return &RoomResource{}
}

func (r *RoomResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_room"
}

func (r *RoomResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Matrix room. Created through the Client-Server API and deleted " +
			"through the Synapse admin API, which is the only way a room can actually be removed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Room ID assigned by the homeserver, for example `!abcdef:example.org`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the room (`m.room.name`).",
				Optional:            true,
			},
			"topic": schema.StringAttribute{
				MarkdownDescription: "Topic of the room (`m.room.topic`).",
				Optional:            true,
			},
			"preset": schema.StringAttribute{
				MarkdownDescription: "Initial join rules and history visibility. One of `private_chat`, " +
					"`trusted_private_chat` or `public_chat`. Applied only at creation, because it is " +
					"shorthand for state events that Matrix has no way to re-apply afterwards.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("private_chat", "trusted_private_chat", "public_chat"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"visibility": schema.StringAttribute{
				MarkdownDescription: "Whether the room is listed in the public room directory: `public` " +
					"or `private`. This is independent of the room's join rules — an invite-only room " +
					"can still be listed.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf(matrix.VisibilityPublic, matrix.VisibilityPrivate),
				},
			},
			"alias_localpart": schema.StringAttribute{
				MarkdownDescription: "Local part of the room's initial alias, without the leading `#` " +
					"or the `:server` suffix. Set at creation only.",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"room_version": schema.StringAttribute{
				MarkdownDescription: "Room version to create, for example `10`. Defaults to the " +
					"homeserver's preferred version. Cannot be changed after creation; Matrix upgrades " +
					"a room by replacing it with a new one.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"encryption": schema.BoolAttribute{
				MarkdownDescription: "Enable end-to-end encryption (`m.room.encryption`). Defaults to " +
					"`true`; set it to `false` for rooms a bot or bridge must read, since those need " +
					"working E2EE support and a device store that survives a restart." +
					"\n\n~> **Encryption cannot be switched off.** Matrix has no way to remove the " +
					"state event once written, so changing this from `true` to `false` destroys and " +
					"recreates the room, losing its history. Turning it on for an existing room is done " +
					"in place.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplaceIf(
						func(_ context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
							// Only disabling forces replacement; enabling is a state write.
							resp.RequiresReplace = req.StateValue.ValueBool() && !req.PlanValue.ValueBool()
						},
						"If encryption is disabled, Terraform will destroy and recreate the room.",
						"If encryption is disabled, Terraform will destroy and recreate the room.",
					),
				},
			},
			"block_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "Prevent the room being rejoined or recreated under the same ID " +
					"after it is destroyed. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"purge_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "Remove the room's history from the homeserver database on destroy. " +
					"Defaults to `true`; set to `false` to leave history in place after the room is removed.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
		},
	}
}

func (r *RoomResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RoomResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoomResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	createReq := matrix.CreateRoomRequest{
		Name:          plan.Name.ValueString(),
		Topic:         plan.Topic.ValueString(),
		Preset:        plan.Preset.ValueString(),
		Visibility:    plan.Visibility.ValueString(),
		RoomAliasName: plan.AliasLocalpart.ValueString(),
		RoomVersion:   plan.RoomVersion.ValueString(),
	}

	// Set at creation rather than afterwards so there is no window in which the
	// room exists unencrypted.
	if plan.Encryption.ValueBool() {
		createReq.InitialState = []matrix.StateEvent{{
			Type:    eventRoomEncryption,
			Content: json.RawMessage(fmt.Sprintf(`{"algorithm":%q}`, megolmV1)),
		}}
	}

	roomID, err := r.client.CreateRoom(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create room", err.Error())

		return
	}

	plan.ID = types.StringValue(roomID)

	// The homeserver picks the version and the directory listing when the
	// configuration does not, so read both back rather than leaving them unknown.
	if err := r.readComputed(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Room created but cannot be read back", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RoomResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoomResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	roomID := state.ID.ValueString()

	if _, err := r.client.GetRoomDetails(ctx, roomID); err != nil {
		if matrix.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Cannot read room", err.Error())

		return
	}

	name, err := r.client.GetStateString(ctx, roomID, eventRoomName, "name")
	if err != nil {
		resp.Diagnostics.AddError("Cannot read room name", err.Error())

		return
	}

	topic, err := r.client.GetStateString(ctx, roomID, eventRoomTopic, "topic")
	if err != nil {
		resp.Diagnostics.AddError("Cannot read room topic", err.Error())

		return
	}

	// An absent name or topic is null rather than "", so that a room which never
	// had one does not read as drift against a configuration that omits it.
	state.Name = optionalString(name, state.Name)
	state.Topic = optionalString(topic, state.Topic)

	encrypted, err := r.client.HasState(ctx, roomID, eventRoomEncryption)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read room encryption", err.Error())

		return
	}

	state.Encryption = types.BoolValue(encrypted)

	visibility, err := r.client.GetDirectoryVisibility(ctx, roomID)
	if err != nil && !matrix.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot read room directory visibility", err.Error())

		return
	}

	if visibility != "" {
		state.Visibility = types.StringValue(visibility)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RoomResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RoomResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	roomID := state.ID.ValueString()
	plan.ID = state.ID

	if !plan.Name.Equal(state.Name) {
		if err := r.setStateString(ctx, roomID, eventRoomName, "name", plan.Name); err != nil {
			resp.Diagnostics.AddError("Cannot update room name", err.Error())

			return
		}
	}

	if !plan.Topic.Equal(state.Topic) {
		if err := r.setStateString(ctx, roomID, eventRoomTopic, "topic", plan.Topic); err != nil {
			resp.Diagnostics.AddError("Cannot update room topic", err.Error())

			return
		}
	}

	if plan.Encryption.ValueBool() && !state.Encryption.ValueBool() {
		content := json.RawMessage(fmt.Sprintf(`{"algorithm":%q}`, megolmV1))
		if _, err := r.client.SetState(ctx, roomID, eventRoomEncryption, "", content); err != nil {
			resp.Diagnostics.AddError("Cannot enable room encryption", err.Error())

			return
		}
	}

	if !plan.Visibility.Equal(state.Visibility) && !plan.Visibility.IsUnknown() {
		if err := r.client.SetDirectoryVisibility(ctx, roomID, plan.Visibility.ValueString()); err != nil {
			resp.Diagnostics.AddError("Cannot update room directory visibility", err.Error())

			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RoomResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoomResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	deleteReq := matrix.DeleteRoomRequest{
		Block: state.Block.ValueBool(),
		Purge: state.Purge.ValueBool(),
	}

	err := r.client.DeleteRoom(ctx, state.ID.ValueString(), deleteReq)
	if err == nil || matrix.IsNotFound(err) {
		return
	}

	if matrix.IsUnrecognized(err) {
		resp.Diagnostics.AddError(
			"Homeserver cannot delete rooms",
			"The homeserver does not serve the Synapse admin room deletion endpoint, and the "+
				"Client-Server API has no way to delete a room. The room is still present on the "+
				"server and must be removed by hand.",
		)

		return
	}

	resp.Diagnostics.AddError("Cannot delete room", err.Error())
}

func (r *RoomResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readComputed fills in the attributes the homeserver decides.
func (r *RoomResource) readComputed(ctx context.Context, model *RoomResourceModel) error {
	details, err := r.client.GetRoomDetails(ctx, model.ID.ValueString())
	if err != nil {
		return err
	}

	model.RoomVersion = types.StringValue(details.RoomVersion)

	visibility, err := r.client.GetDirectoryVisibility(ctx, model.ID.ValueString())
	if err != nil {
		if matrix.IsNotFound(err) {
			model.Visibility = types.StringValue(matrix.VisibilityPrivate)

			return nil
		}

		return err
	}

	model.Visibility = types.StringValue(visibility)

	return nil
}

// setStateString writes a single-field state event, or clears it when the
// configured value is null.
func (r *RoomResource) setStateString(ctx context.Context, roomID, eventType, field string, value types.String) error {
	content := map[string]string{}
	if !value.IsNull() {
		content[field] = value.ValueString()
	}

	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}

	_, err = r.client.SetState(ctx, roomID, eventType, "", encoded)

	return err
}

// optionalString keeps an absent server-side value null when configuration also
// left it unset, so that "" and null do not produce spurious diffs.
func optionalString(value string, current types.String) types.String {
	if value == "" && current.IsNull() {
		return current
	}

	if value == "" {
		return types.StringNull()
	}

	return types.StringValue(value)
}
