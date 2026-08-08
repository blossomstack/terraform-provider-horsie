package provider

import (
	"context"
	"fmt"
	"net/http"

	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

var (
	_ resource.Resource                = (*agentResource)(nil)
	_ resource.ResourceWithConfigure   = (*agentResource)(nil)
	_ resource.ResourceWithImportState = (*agentResource)(nil)
)

type agentResource struct{ client *client.Client }

// NewAgentResource registers `horsie_agent`.
func NewAgentResource() resource.Resource { return &agentResource{} }

type agentModel struct {
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Model          types.String `tfsdk:"model"`
	Plugins        types.List   `tfsdk:"plugins"`
	MCPServers     types.List   `tfsdk:"mcp_servers"`
	MemorySpaces   types.List   `tfsdk:"memory_spaces"`
	ThinkingEffort types.String `tfsdk:"thinking_effort"`
}

func (r *agentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *agentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A named agent preset: a saved agent configuration invoked with a message.\n\n" +
			"A preset says nothing about *where* the work happens. Which machine runs it, and what it " +
			"runs against, are properties of the invocation rather than of the saved configuration: a " +
			"pinned runtime is invisible once it disconnects but fatal at invoke, and a hardcoded " +
			"checkout can only ever be run one way. Use `horsie_environment` for that.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug used in API paths and CLI invocations. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "What this preset is for. Defaults to empty.",
			},
			"model": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A `horsie_model` alias. Reference the resource's `alias` so Terraform orders the two correctly.",
			},
			"plugins": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Skill bundles available to sessions from this preset.",
			},
			"mcp_servers": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Names of the MCP servers this preset enables.",
			},
			"memory_spaces": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "`horsie_memory_space` names the agent may read and write.",
			},
			"thinking_effort": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Canonical thinking effort. Omit to take the model's configured default; " +
					"it must be one the model offers.",
			},
		},
	}
}

func (r *agentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	r.client = c
}

func stringsFromList(ctx context.Context, l types.List) *[]string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	if diags := l.ElementsAs(ctx, &out, false); diags.HasError() {
		return nil
	}
	return &out
}

func listFromStrings(ctx context.Context, v []string) types.List {
	if len(v) == 0 {
		return types.ListNull(types.StringType)
	}
	l, _ := types.ListValueFrom(ctx, types.StringType, v)
	return l
}

func (r *agentResource) input(ctx context.Context, m agentModel) api.AgentPresetInput {
	in := api.AgentPresetInput{Name: m.Name.ValueString(), Model: m.Model.ValueString()}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		v := m.Description.ValueString()
		in.Description = &v
	}
	in.Plugins = stringsFromList(ctx, m.Plugins)
	in.MCPServers = stringsFromList(ctx, m.MCPServers)
	in.MemorySpaces = stringsFromList(ctx, m.MemorySpaces)
	if !m.ThinkingEffort.IsNull() {
		v := m.ThinkingEffort.ValueString()
		in.ThinkingEffort = &v
	}
	return in
}

func applyAgent(ctx context.Context, m *agentModel, v *api.AgentView) {
	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.Model = types.StringValue(v.Model)
	m.Plugins = listFromStrings(ctx, v.Plugins)
	m.MCPServers = listFromStrings(ctx, v.MCPServers)
	m.MemorySpaces = listFromStrings(ctx, v.MemorySpaces)
	m.ThinkingEffort = optString(v.ThinkingEffort)
}

func (r *agentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.CreateAgent(ctx, r.input(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not create agent preset", err.Error())
		return
	}
	applyAgent(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetAgent(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read agent preset", err.Error())
		return
	}
	applyAgent(ctx, &state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *agentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan agentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.ReplaceAgent(ctx, plan.Name.ValueString(), r.input(ctx, plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not update agent preset", err.Error())
		return
	}
	applyAgent(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *agentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state agentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteAgent(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		return
	}
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		resp.Diagnostics.AddError(
			"Agent preset is still in use",
			"horsie refuses to delete a preset a routine still runs. Destroy the horsie_routine "+
				"resources first — Terraform does that automatically if they reference this one.\n\n"+apiErr.Error(),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not delete agent preset", err.Error())
	}
}

func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
