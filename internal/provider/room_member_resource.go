// Copyright (c) Pushkar Anand
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

// memberIDSeparator joins the room and user IDs into the resource ID.
//
// A comma, because the obvious choice does not work: a user ID localpart may
// legitimately contain "/", so a slash-separated ID cannot be split back apart
// unambiguously.
const memberIDSeparator = ","

var (
	_ resource.Resource                   = &RoomMemberResource{}
	_ resource.ResourceWithConfigure      = &RoomMemberResource{}
	_ resource.ResourceWithImportState    = &RoomMemberResource{}
	_ resource.ResourceWithValidateConfig = &RoomMemberResource{}
)

// userIDPattern matches a fully qualified Matrix user ID.
var userIDPattern = regexp.MustCompile(`^@[a-z0-9._=/+-]+:.+$`)

// RoomMemberResource manages one user's membership of one room.
type RoomMemberResource struct {
	client *matrix.Client
}

// RoomMemberResourceModel describes the room member resource data model.
type RoomMemberResourceModel struct {
	ID                types.String `tfsdk:"id"`
	RoomID            types.String `tfsdk:"room_id"`
	UserID            types.String `tfsdk:"user_id"`
	Membership        types.String `tfsdk:"membership"`
	CurrentMembership types.String `tfsdk:"current_membership"`
	Reason            types.String `tfsdk:"reason"`
}

func NewRoomMemberResource() resource.Resource {
	return &RoomMemberResource{}
}

func (r *RoomMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_room_member"
}

func (r *RoomMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Membership of one user in one room.\n\n" +
			"Inviting, kicking and banning go through the Client-Server API. `join` additionally " +
			"needs the Synapse admin API, because the Matrix auth rules only accept a `join` " +
			"membership event from the joining user themselves — an administrator can invite an " +
			"account but never accept on its behalf. That endpoint does not invite either, so in a " +
			"room that admits people by invitation the provider sends one first.\n\n" +
			"The account the provider authenticates as must be in the room and hold enough power " +
			"for the action being taken.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Room ID and user ID joined by a comma, " +
					"for example `!abcdef:example.org,@alice:example.org`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"room_id": schema.StringAttribute{
				MarkdownDescription: "ID of the room, for example `!abcdef:example.org`.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "Full user ID of the member, for example `@alice:example.org`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						userIDPattern,
						"must be a full Matrix user ID, of the form @localpart:server",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"membership": schema.StringAttribute{
				MarkdownDescription: "Membership to enforce: `invite`, `join` or `ban`. Defaults to `join`." +
					"\n\n~> **`invite` is a floor, not an exact value.** Terraform can withdraw an " +
					"invitation but cannot un-accept one, so a user who accepts is not drift and does " +
					"not get removed on the next apply. Read `current_membership` for what the " +
					"homeserver actually holds.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(matrix.MembershipJoin),
				Validators: []validator.String{
					stringvalidator.OneOf(matrix.MembershipInvite, matrix.MembershipJoin, matrix.MembershipBan),
				},
			},
			"current_membership": schema.StringAttribute{
				MarkdownDescription: "Membership the homeserver currently holds for this user: " +
					"`invite`, `join`, `leave`, `ban` or `knock`.",
				Computed: true,
			},
			"reason": schema.StringAttribute{
				MarkdownDescription: "Reason recorded on the membership event, shown to the user and " +
					"in the room timeline. Applied when the membership is set; changing it alone does " +
					"not rewrite an event already sent.",
				Optional: true,
			},
		},
	}
}

func (r *RoomMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig rejects a forced join of a remote user.
//
// The admin join endpoint is limited to local accounts, and a homeserver has no
// way to make a user on another server join anything. Catching it here turns an
// apply-time server rejection into a plan-time error naming the cause.
func (r *RoomMemberResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config RoomMemberResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	if config.UserID.IsNull() || config.UserID.IsUnknown() {
		return
	}

	// Null means the default, which is join. Unknown could still resolve to
	// anything, so it is not ours to reject.
	if config.Membership.IsUnknown() {
		return
	}

	if !config.Membership.IsNull() && config.Membership.ValueString() != matrix.MembershipJoin {
		return
	}

	if r.client.IsLocalUser(config.UserID.ValueString()) {
		return
	}

	resp.Diagnostics.AddAttributeError(
		path.Root("membership"),
		"Cannot force a remote user to join",
		fmt.Sprintf(
			"%s is not local to %s, and a homeserver cannot accept an invitation on behalf of a "+
				"user it does not host. Use membership = %q and let them accept it themselves.",
			config.UserID.ValueString(), r.client.ServerName(), matrix.MembershipInvite,
		),
	)
}

func (r *RoomMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoomMemberResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Cannot set room membership", err.Error())

		return
	}

	plan.ID = types.StringValue(memberID(plan.RoomID.ValueString(), plan.UserID.ValueString()))

	if err := r.readCurrent(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Membership set but cannot be read back", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RoomMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoomMemberResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	current, err := r.client.GetMembership(ctx, state.RoomID.ValueString(), state.UserID.ValueString())
	if err != nil {
		// A deleted room takes its membership with it; there is nothing left to
		// converge on and the resource is gone rather than drifted.
		if matrix.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Cannot read room membership", err.Error())

		return
	}

	state.CurrentMembership = types.StringValue(current)

	// Report drift only when the server has fallen below what was asked for.
	// Writing the observed value into `membership` is what makes Terraform plan a
	// change back to the configured one; leaving the configured value in place
	// when it is satisfied is what stops an accepted invitation looking like
	// drift on every plan.
	if !satisfies(current, state.Membership.ValueString()) {
		state.Membership = types.StringValue(current)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RoomMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RoomMemberResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID

	if err := r.apply(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Cannot update room membership", err.Error())

		return
	}

	if err := r.readCurrent(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Membership set but cannot be read back", err.Error())

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RoomMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoomMemberResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	roomID, userID := state.RoomID.ValueString(), state.UserID.ValueString()
	reason := state.Reason.ValueString()

	current, err := r.client.GetMembership(ctx, roomID, userID)
	if err != nil {
		if matrix.IsNotFound(err) {
			return
		}

		resp.Diagnostics.AddError("Cannot read room membership", err.Error())

		return
	}

	// Already out of the room: kicking from "leave" is rejected rather than
	// ignored, so a destroy after a manual removal would fail on the way out.
	if current == matrix.MembershipLeave {
		return
	}

	// Both branches land on "leave", which is as close to absent as a membership
	// gets: Matrix keeps the member event forever and only ever rewrites it.
	if current == matrix.MembershipBan {
		err = r.client.UnbanUser(ctx, roomID, userID, reason)
	} else {
		err = r.client.KickUser(ctx, roomID, userID, reason)
	}

	// A room that is already gone, or a user already out of it, is the state
	// destroy was asking for.
	if err == nil || matrix.IsNotFound(err) {
		return
	}

	resp.Diagnostics.AddError("Cannot remove room member", err.Error())
}

func (r *RoomMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	roomID, userID, ok := cutMemberID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf(
				"Expected a room ID and a user ID separated by %q, for example "+
					"\"!abcdef:example.org%s@alice:example.org\". Got %q.",
				memberIDSeparator, memberIDSeparator, req.ID,
			),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("room_id"), roomID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}

// apply moves the membership to what the model asks for.
//
// It reads the server's current value rather than trusting state, because every
// endpoint below is a transition rather than an assignment: inviting someone
// already invited, or unbanning someone who is not banned, is an error and not
// a no-op.
func (r *RoomMemberResource) apply(ctx context.Context, model RoomMemberResourceModel) error {
	roomID, userID := model.RoomID.ValueString(), model.UserID.ValueString()
	reason := model.Reason.ValueString()
	target := model.Membership.ValueString()

	current, err := r.client.GetMembership(ctx, roomID, userID)
	if err != nil {
		return fmt.Errorf("reading the current membership: %w", err)
	}

	// A ban has to be lifted before anything else can be set: while it stands,
	// the auth rules reject an invite outright.
	if current == matrix.MembershipBan && target != matrix.MembershipBan {
		if err := r.client.UnbanUser(ctx, roomID, userID, reason); err != nil {
			return fmt.Errorf("lifting the existing ban: %w", err)
		}

		current = matrix.MembershipLeave
	}

	if satisfies(current, target) {
		return nil
	}

	switch target {
	case matrix.MembershipInvite:
		return r.client.InviteUser(ctx, roomID, userID, reason)
	case matrix.MembershipBan:
		return r.client.BanUser(ctx, roomID, userID, reason)
	case matrix.MembershipJoin:
		// The admin endpoint appends the member event as the target user and puts
		// it through the ordinary auth checks; it does not invite on the way. In
		// a room that admits people by invitation, an uninvited account therefore
		// fails those checks, so the invitation has to come first -- from us, who
		// are in the room and hold the power to send it.
		if current != matrix.MembershipInvite {
			needed, err := r.needsInviteToJoin(ctx, roomID)
			if err != nil {
				return err
			}

			if needed {
				if err := r.client.InviteUser(ctx, roomID, userID, reason); err != nil {
					return fmt.Errorf("inviting before the join: %w", err)
				}
			}
		}

		if err := r.client.AdminJoinUser(ctx, roomID, userID); err != nil {
			if matrix.IsUnrecognized(err) {
				return fmt.Errorf(
					"the homeserver does not serve the Synapse admin join endpoint, and the "+
						"Client-Server API cannot join a room on another user's behalf: %w", err)
			}

			return err
		}

		return nil
	default:
		return fmt.Errorf("unsupported membership %q", target)
	}
}

// needsInviteToJoin reports whether the room turns away an uninvited user.
//
// Only a public room lets anyone in unasked. A restricted room admits members
// of another room, but an invitation is accepted there too, so treating it like
// the rest costs nothing and avoids depending on which room the account is in.
func (r *RoomMemberResource) needsInviteToJoin(ctx context.Context, roomID string) (bool, error) {
	rule, err := r.client.GetJoinRule(ctx, roomID)
	if err != nil {
		return false, fmt.Errorf("reading the room's join rule: %w", err)
	}

	return rule != matrix.JoinRulePublic, nil
}

// readCurrent fills in current_membership from the homeserver.
func (r *RoomMemberResource) readCurrent(ctx context.Context, model *RoomMemberResourceModel) error {
	current, err := r.client.GetMembership(ctx, model.RoomID.ValueString(), model.UserID.ValueString())
	if err != nil {
		return err
	}

	model.CurrentMembership = types.StringValue(current)

	return nil
}

// satisfies reports whether an observed membership already meets the configured
// one, so that no action is taken and no drift is reported.
//
// Only invite is a floor rather than an exact match, and only because of an
// asymmetry in what Terraform can do: it can withdraw an invitation, but there
// is no way to un-accept one, so treating an accepted invite as drift would
// plan a removal the user never asked for on every apply.
func satisfies(current, target string) bool {
	if current == target {
		return true
	}

	return target == matrix.MembershipInvite && current == matrix.MembershipJoin
}

// memberID builds the composite resource ID.
func memberID(roomID, userID string) string {
	return roomID + memberIDSeparator + userID
}

// cutMemberID splits a composite resource ID back into its two halves.
func cutMemberID(id string) (roomID, userID string, ok bool) {
	roomID, userID, found := strings.Cut(id, memberIDSeparator)

	return roomID, userID, found && roomID != "" && userID != ""
}
