package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = (*modelResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelResource)(nil)
	_ resource.ResourceWithImportState = (*modelResource)(nil)
)

type modelResource struct{ client *client.Client }

// NewModelResource registers `horsie_model`.
func NewModelResource() resource.Resource { return &modelResource{} }

type modelModel struct {
	Alias                      types.String `tfsdk:"alias"`
	Provider                   types.String `tfsdk:"model_provider"`
	ModelID                    types.String `tfsdk:"model_id"`
	MaxTokens                  types.Int64  `tfsdk:"max_tokens"`
	ContextWindow              types.Int64  `tfsdk:"context_window"`
	ThinkingEfforts            types.List   `tfsdk:"thinking_efforts"`
	ThinkingEffort             types.String `tfsdk:"thinking_effort"`
	ThinkingDialect            types.String `tfsdk:"thinking_dialect"`
	ForcedToolsDisableThinking types.Bool   `tfsdk:"forced_tools_disable_thinking"`
}

func (r *modelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (r *modelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A model alias sessions and agent presets select by name.",
		Attributes: map[string]schema.Attribute{
			"alias": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name sessions select, e.g. `sonnet`. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"model_provider": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Name of the `horsie_model_provider` this routes to. " +
					"The server rejects a model naming a provider that does not exist, so reference " +
					"the resource's `name` to let Terraform order the two correctly.",
			},
			"model_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The provider's own model identifier, e.g. `claude-sonnet-4-6`.",
			},
			"max_tokens": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Cap on output tokens per request.",
			},
			"context_window": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Total context window in tokens. horsie fills in a built-in default " +
					"for model ids it recognises, which is why this is computed when omitted.",
			},
			"thinking_efforts": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Thinking-effort levels this model offers, in ascending order. Omit for a model with no thinking control.",
			},
			"thinking_effort": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Default effort when a session picks none. Must be one of `thinking_efforts`.",
			},
			"thinking_dialect": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Wire encoding for this model's thinking control.",
			},
			"forced_tools_disable_thinking": schema.BoolAttribute{
				Optional: true,
				// Computed because the server answers with `false` rather than
				// echoing the omission, and a plain Optional attribute that the
				// server fills in fails the apply with "Provider produced
				// inconsistent result after apply".
				Computed:            true,
				MarkdownDescription: "Set for backends that reject a pinned `tool_choice` while thinking is enabled. Defaults to false.",
			},
		},
	}
}

func (r *modelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *modelResource) input(ctx context.Context, m modelModel) (api.ModelInput, error) {
	in := api.ModelInput{
		Alias:    m.Alias.ValueString(),
		Provider: m.Provider.ValueString(),
		ModelID:  m.ModelID.ValueString(),
	}
	if !m.MaxTokens.IsNull() {
		v := uint32(m.MaxTokens.ValueInt64())
		in.MaxTokens = &v
	}
	if !m.ContextWindow.IsNull() && !m.ContextWindow.IsUnknown() {
		v := uint32(m.ContextWindow.ValueInt64())
		in.ContextWindow = &v
	}
	if !m.ThinkingEfforts.IsNull() && !m.ThinkingEfforts.IsUnknown() {
		var efforts []string
		if diags := m.ThinkingEfforts.ElementsAs(ctx, &efforts, false); diags.HasError() {
			return in, fmt.Errorf("thinking_efforts is not a list of strings")
		}
		in.ThinkingEfforts = &efforts
	}
	if !m.ThinkingEffort.IsNull() {
		v := m.ThinkingEffort.ValueString()
		in.ThinkingEffort = &v
	}
	if !m.ThinkingDialect.IsNull() {
		v := m.ThinkingDialect.ValueString()
		in.ThinkingDialect = &v
	}
	if !m.ForcedToolsDisableThinking.IsNull() {
		v := m.ForcedToolsDisableThinking.ValueBool()
		in.ForcedToolsDisableThinking = &v
	}
	return in, nil
}

// applyModel copies the server's view back, keeping the optional fields the
// caller left unset as null so an omitted attribute does not show as drift.
func applyModel(ctx context.Context, m *modelModel, v *api.ModelView) {
	m.Alias = types.StringValue(v.Alias)
	m.Provider = types.StringValue(v.Provider)
	m.ModelID = types.StringValue(v.ModelID)
	m.MaxTokens = optInt64(v.MaxTokens)
	m.ContextWindow = optInt64(v.ContextWindow)
	if v.ThinkingEfforts != nil {
		list, _ := types.ListValueFrom(ctx, types.StringType, *v.ThinkingEfforts)
		m.ThinkingEfforts = list
	} else {
		m.ThinkingEfforts = types.ListNull(types.StringType)
	}
	m.ThinkingEffort = optString(v.ThinkingEffort)
	m.ThinkingDialect = optString(v.ThinkingDialect)
	if v.ForcedToolsDisableThinking != nil {
		m.ForcedToolsDisableThinking = types.BoolValue(*v.ForcedToolsDisableThinking)
	} else {
		m.ForcedToolsDisableThinking = types.BoolNull()
	}
}

func optInt64(v *uint32) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

func optString(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func (r *modelResource) write(ctx context.Context, plan *modelModel) error {
	in, err := r.input(ctx, *plan)
	if err != nil {
		return err
	}
	view, err := r.client.PutModel(ctx, plan.Alias.ValueString(), in)
	if err != nil {
		return err
	}
	applyModel(ctx, plan, view)
	return nil
}

func (r *modelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Could not create model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetModel(ctx, state.Alias.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read model", err.Error())
		return
	}
	applyModel(ctx, &state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Could not update model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteModel(ctx, state.Alias.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete model", err.Error())
	}
}

func (r *modelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("alias"), req, resp)
}
