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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pushkar-anand/terraform-provider-matrix/internal/matrix"
)

// localpartPattern is the character set Matrix permits in a user ID localpart.
var localpartPattern = regexp.MustCompile(`^[a-z0-9._=/+-]+$`)

var (
	_ resource.Resource                   = &UserResource{}
	_ resource.ResourceWithConfigure      = &UserResource{}
	_ resource.ResourceWithImportState    = &UserResource{}
	_ resource.ResourceWithValidateConfig = &UserResource{}
)

// UserResource manages a local account on the homeserver.
type UserResource struct {
	client *matrix.Client
}

// UserResourceModel describes the user resource data model.
type UserResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Localpart       types.String `tfsdk:"localpart"`
	DisplayName     types.String `tfsdk:"display_name"`
	AvatarURL       types.String `tfsdk:"avatar_url"`
	Admin           types.Bool   `tfsdk:"admin"`
	Deactivated     types.Bool   `tfsdk:"deactivated"`
	Locked          types.Bool   `tfsdk:"locked"`
	Password        types.String `tfsdk:"password_wo"`
	PasswordVersion types.String `tfsdk:"password_wo_version"`
	LogoutDevices   types.Bool   `tfsdk:"logout_devices_on_password_change"`
	EraseOnDestroy  types.Bool   `tfsdk:"erase_on_destroy"`
}

func NewUserResource() resource.Resource {
	return &UserResource{}
}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A local account on the homeserver, managed through the Synapse admin API. " +
			"The provider's access token must belong to a server administrator." +
			"\n\n~> **Destroying this resource deactivates the account; it does not delete it.** " +
			"Matrix has no delete for users, and a deactivated account still occupies its user ID " +
			"permanently — the localpart can never be registered again, by Terraform or otherwise.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Full Matrix user ID, for example `@alice:example.org`. Built from " +
					"`localpart` and the homeserver's own name, which is read from the provider's token " +
					"rather than from the URL.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"localpart": schema.StringAttribute{
				MarkdownDescription: "Local part of the user ID, without the leading `@` or the " +
					"`:server` suffix. Matrix cannot rename an account, so changing this destroys the " +
					"old one and creates a new one.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						localpartPattern,
						"must contain only lowercase letters, digits, and the characters . _ = - / +",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Display name shown to other users. Removing this from the " +
					"configuration leaves the current value in place; set it to `\"\"` to clear it.",
				Optional: true,
				Computed: true,
			},
			"avatar_url": schema.StringAttribute{
				MarkdownDescription: "Avatar as an `mxc://` URI. Removing this from the configuration " +
					"leaves the current value in place; set it to `\"\"` to clear it.",
				Optional: true,
				Computed: true,
			},
			"admin": schema.BoolAttribute{
				MarkdownDescription: "Whether the account is a server administrator. Defaults to `false`." +
					"\n\n~> A homeserver refuses to demote the account the provider itself " +
					"authenticates as, so do not manage the provider's own user with `admin = false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"deactivated": schema.BoolAttribute{
				MarkdownDescription: "Whether the account is deactivated. Defaults to `false`, which " +
					"means an account deactivated outside Terraform is reactivated on the next apply." +
					"\n\n~> Reactivating an account without also supplying `password_wo` leaves it " +
					"with no usable password, because the homeserver writes a placeholder that no " +
					"login can match.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"locked": schema.BoolAttribute{
				MarkdownDescription: "Whether the account is locked. A locked account keeps its data " +
					"and sessions but cannot use them. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"password_wo": schema.StringAttribute{
				MarkdownDescription: "Password for the account. This is a " +
					"[write-only argument](https://developer.hashicorp.com/terraform/language/resources/ephemeral/write-only): " +
					"Terraform sends it to the provider but never persists it, so it does not appear " +
					"in state or in plan files and can be sourced from a secret manager safely. " +
					"Requires Terraform 1.11 or later." +
					"\n\nBecause the value is never stored, Terraform cannot tell when it changes. " +
					"Change `password_wo_version` to make the provider send it again.",
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				Validators: []validator.String{
					stringvalidator.AlsoRequires(path.MatchRoot("password_wo_version")),
				},
			},
			"password_wo_version": schema.StringAttribute{
				MarkdownDescription: "Arbitrary version marker for `password_wo`. Changing it is what " +
					"triggers the password being sent again; on its own it has no meaning to the " +
					"homeserver.",
				Optional: true,
			},
			"logout_devices_on_password_change": schema.BoolAttribute{
				MarkdownDescription: "End every existing session when the password is set. Defaults to " +
					"`true`, matching the homeserver's own default. Set to `false` to rotate a " +
					"password without signing the account out everywhere.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"erase_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "Erase the account's data when it is destroyed, rather than only " +
					"deactivating it. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig rejects the one combination the homeserver refuses, so it
// fails during plan rather than halfway through an apply.
func (r *UserResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config UserResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if config.Deactivated.ValueBool() && config.Locked.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("locked"),
			"Account cannot be both deactivated and locked",
			"The homeserver rejects an account that is deactivated and locked at once. "+
				"Deactivation already prevents all access, so set only one of the two.",
		)
	}
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	// Write-only values reach the provider through the configuration alone; the
	// plan carries them as null by design.
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	userID := r.client.UserIDFor(plan.Localpart.ValueString())

	upsert := matrix.UpsertUserRequest{
		Deactivated: ptr(plan.Deactivated.ValueBool()),
		Locked:      ptr(plan.Locked.ValueBool()),
	}

	// admin:false is deliberately not sent. The homeserver maps it onto a revoke,
	// and revoking from an account that was never an admin is an error -- which
	// is every account this has just created. Demotion is handled after the
	// response, in the one case where there is anything to demote.
	if plan.Admin.ValueBool() {
		upsert.Admin = ptr(true)
	}

	if !config.Password.IsNull() {
		upsert.Password = ptr(config.Password.ValueString())
		upsert.LogoutDevices = ptr(plan.LogoutDevices.ValueBool())
	}

	// An unknown value here is a computed attribute the configuration left out;
	// omitting it lets the homeserver choose rather than forcing an empty one.
	if isSet(plan.DisplayName) {
		upsert.DisplayName = ptr(plan.DisplayName.ValueString())
	}

	if isSet(plan.AvatarURL) {
		upsert.AvatarURL = ptr(plan.AvatarURL.ValueString())
	}

	details, err := r.client.UpsertUser(ctx, userID, upsert)
	if err != nil {
		if matrix.IsUnrecognized(err) {
			resp.Diagnostics.AddError(
				"Homeserver cannot manage users",
				"The homeserver does not serve the Synapse admin user endpoints, so this provider "+
					"cannot create accounts on it.",
			)

			return
		}

		resp.Diagnostics.AddError("Cannot create user", err.Error())

		return
	}

	// The endpoint is an upsert, so it may have adopted an account that already
	// existed -- and that one can be an admin already. This is the only case where
	// a revoke has anything to act on.
	if !plan.Admin.ValueBool() && bool(details.Admin) {
		details, err = r.client.UpsertUser(ctx, userID, matrix.UpsertUserRequest{Admin: ptr(false)})
		if err != nil {
			resp.Diagnostics.AddError("User created but cannot be demoted from admin", err.Error())

			return
		}
	}

	plan.ID = types.StringValue(userID)
	applyUserDetails(&plan, details)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	details, err := r.client.GetUser(ctx, state.ID.ValueString())
	if err != nil {
		if matrix.IsNotFound(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		resp.Diagnostics.AddError("Cannot read user", err.Error())

		return
	}

	applyUserDetails(&state, details)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)

	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID

	upsert := matrix.UpsertUserRequest{}

	if !plan.Admin.Equal(state.Admin) {
		upsert.Admin = ptr(plan.Admin.ValueBool())
	}

	if !plan.Deactivated.Equal(state.Deactivated) {
		upsert.Deactivated = ptr(plan.Deactivated.ValueBool())
	}

	if !plan.Locked.Equal(state.Locked) {
		upsert.Locked = ptr(plan.Locked.ValueBool())
	}

	if !plan.DisplayName.Equal(state.DisplayName) && isSet(plan.DisplayName) {
		upsert.DisplayName = ptr(plan.DisplayName.ValueString())
	}

	if !plan.AvatarURL.Equal(state.AvatarURL) && isSet(plan.AvatarURL) {
		upsert.AvatarURL = ptr(plan.AvatarURL.ValueString())
	}

	// The password itself is never in state, so there is nothing to compare it
	// against. The version marker is the only signal that it should be resent.
	if !config.Password.IsNull() && !plan.PasswordVersion.Equal(state.PasswordVersion) {
		upsert.Password = ptr(config.Password.ValueString())
		upsert.LogoutDevices = ptr(plan.LogoutDevices.ValueBool())
	}

	details, err := r.client.UpsertUser(ctx, state.ID.ValueString(), upsert)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update user", err.Error())

		return
	}

	applyUserDetails(&plan, details)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeactivateUser(ctx, state.ID.ValueString(), state.EraseOnDestroy.ValueBool())
	if err == nil || matrix.IsNotFound(err) {
		return
	}

	resp.Diagnostics.AddError("Cannot deactivate user", err.Error())
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	userID := req.ID

	localpart, _, found := strings.Cut(strings.TrimPrefix(userID, "@"), ":")
	if !strings.HasPrefix(userID, "@") || !found || localpart == "" {
		resp.Diagnostics.AddError(
			"Invalid user ID",
			fmt.Sprintf("Expected a full Matrix user ID such as @alice:example.org, got %q.", userID),
		)

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), userID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("localpart"), localpart)...)

	// Destroy-time and password-rotation behaviour is local to Terraform; the
	// homeserver has nothing to report for it, so seed the schema defaults
	// rather than leaving these null for the Read that follows.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("erase_on_destroy"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("logout_devices_on_password_change"), true)...)
}

// applyUserDetails copies the homeserver's view of the account into the model.
func applyUserDetails(model *UserResourceModel, details *matrix.UserDetails) {
	if details.Name != "" {
		model.ID = types.StringValue(details.Name)
	}

	model.DisplayName = serverString(details.DisplayName, model.DisplayName)
	model.AvatarURL = serverString(details.AvatarURL, model.AvatarURL)
	model.Admin = types.BoolValue(bool(details.Admin))
	model.Deactivated = types.BoolValue(bool(details.Deactivated))
	model.Locked = types.BoolValue(bool(details.Locked))
}

// serverString reconciles a value the homeserver reports as null against what
// the configuration asked for.
//
// Clearing a field is spelled as "" going out but comes back as null, so a
// plain mapping would make the applied state differ from the plan and fail the
// apply. Keeping the planned "" is the only way both can hold.
func serverString(value *string, planned types.String) types.String {
	if value != nil && *value != "" {
		return types.StringValue(*value)
	}

	if isSet(planned) && planned.ValueString() == "" {
		return planned
	}

	return types.StringNull()
}

// isSet reports whether an attribute carries a usable value, as opposed to
// being absent or still to be decided by the homeserver.
func isSet(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown()
}

func ptr[T any](value T) *T { return &value }
