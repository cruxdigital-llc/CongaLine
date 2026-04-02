package terraform

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

var (
	_ resource.Resource                = &secretResource{}
	_ resource.ResourceWithImportState = &secretResource{}
)

type secretResource struct {
	prov congaprovider.Provider
}

type secretResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Agent types.String `tfsdk:"agent"`
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

func NewSecretResource() resource.Resource {
	return &secretResource{}
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a secret for a CongaLine agent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Secret identifier (agent/name).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"agent": schema.StringAttribute{
				Required:    true,
				Description: "Agent name this secret belongs to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(agentNameRegex, "must be lowercase alphanumeric with hyphens"),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Secret name (kebab-case, e.g. anthropic-api-key).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(secretNameRegex, "must be kebab-case (e.g. anthropic-api-key)"),
				},
			},
			"value": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Secret value. Cannot be read back after creation.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		},
	}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.prov = extractProvider(req, resp)
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	agent := plan.Agent.ValueString()
	name := plan.Name.ValueString()

	if err := r.prov.SetSecret(ctx, agent, name, plan.Value.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to set secret", err.Error())
		return
	}

	plan.ID = types.StringValue(agent + "/" + name)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets, err := r.prov.ListSecrets(ctx, state.Agent.ValueString())
	if err != nil {
		if isNotFoundErr(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to list secrets", err.Error())
		return
	}

	found := false
	for _, s := range secrets {
		if s.Name == state.Name.ValueString() {
			found = true
			break
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	// Value is write-only — preserve from state (cannot be read back).
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.prov.SetSecret(ctx, plan.Agent.ValueString(), plan.Name.ValueString(), plan.Value.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to update secret", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.Agent.ValueString() + "/" + plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.prov.DeleteSecret(ctx, state.Agent.ValueString(), state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete secret", err.Error())
	}
}

func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 2)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected format: agent/secret-name, got: %s", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("agent"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
	// Value cannot be read back from the provider, so it's set to empty string.
	// After import, the user must run `terraform apply` with the correct value in config to reconcile state.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value"), "")...)
	resp.Diagnostics.AddWarning(
		"Secret value not imported",
		fmt.Sprintf("The value for secret %q cannot be read back. Set the correct value in your Terraform configuration and run `terraform apply`.", req.ID),
	)
}
