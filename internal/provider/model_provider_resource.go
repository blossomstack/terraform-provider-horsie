package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
	_ resource.Resource                = (*modelProviderResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelProviderResource)(nil)
	_ resource.ResourceWithImportState = (*modelProviderResource)(nil)
)

type modelProviderResource struct{ client *client.Client }

// NewModelProviderResource registers `horsie_model_provider`.
func NewModelProviderResource() resource.Resource { return &modelProviderResource{} }

type modelProviderModel struct {
	Name                  types.String `tfsdk:"name"`
	Kind                  types.String `tfsdk:"kind"`
	BaseURL               types.String `tfsdk:"base_url"`
	APIKey                types.String `tfsdk:"api_key"`
	KeepThinkingSignature types.Bool   `tfsdk:"keep_thinking_signature"`
	HasCredential         types.Bool   `tfsdk:"has_credential"`
}

func (r *modelProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model_provider"
}

func (r *modelProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An LLM provider a model alias can route to.\n\n" +
			"Named `model_provider` rather than `provider` because horsie already uses " +
			"\"vendor\" for the execution runtime, and because `provider` is a reserved word in HCL.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier a model's `provider` references. Changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{
					// The name is the identity in the URL; horsie refuses a body
					// that disagrees with the path rather than renaming a row,
					// because a silent move would strand the models pointing at
					// the old name.
					stringplanmodifier.RequiresReplace(),
				},
			},
			"kind": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "One of `anthropic`, `openai`, `openai-responses` or `chatgpt`.",
			},
			"base_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Override for the provider's API base URL.",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "API key. Omit to leave a stored key untouched, or set `\"\"` to clear it. " +
					"A `chatgpt` provider stores no key — it authenticates with an OAuth sign-in performed " +
					"out of band, so Terraform cannot manage its credential.",
			},
			"keep_thinking_signature": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Retain thinking-block signatures. Required for genuine Anthropic, which validates them on replay; leave off for Anthropic-compatible endpoints, which do not.",
			},
			"has_credential": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the provider can authenticate at all. The server never returns the key itself.",
			},
		},
	}
}

func (r *modelProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *modelProviderResource) input(m modelProviderModel) api.ProviderInput {
	in := api.ProviderInput{Name: m.Name.ValueString(), Kind: m.Kind.ValueString()}
	if !m.BaseURL.IsNull() {
		v := m.BaseURL.ValueString()
		in.BaseURL = &v
	}
	// Null means "not managed here", which is exactly horsie's "omitted keeps
	// the stored key". An empty string is a deliberate clear and is sent.
	if !m.APIKey.IsNull() {
		v := m.APIKey.ValueString()
		in.APIKey = &v
	}
	if !m.KeepThinkingSignature.IsNull() && !m.KeepThinkingSignature.IsUnknown() {
		v := m.KeepThinkingSignature.ValueBool()
		in.KeepThinkingSignature = &v
	}
	return in
}

// apply copies the server's view back into state, leaving api_key alone: the
// server never returns it, so state keeps whatever the configuration said.
func apply(m *modelProviderModel, v *api.ProviderView) {
	m.Name = types.StringValue(v.Name)
	m.Kind = types.StringValue(v.Kind)
	if v.BaseURL != nil {
		m.BaseURL = types.StringValue(*v.BaseURL)
	} else {
		m.BaseURL = types.StringNull()
	}
	m.KeepThinkingSignature = types.BoolValue(v.KeepThinkingSignature)
	m.HasCredential = types.BoolValue(v.HasCredential)
}

func (r *modelProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.PutModelProvider(ctx, plan.Name.ValueString(), r.input(plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not create model provider", err.Error())
		return
	}
	apply(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetModelProvider(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		// Deleted outside Terraform: drop it so the next plan recreates it.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read model provider", err.Error())
		return
	}
	apply(&state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *modelProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.PutModelProvider(ctx, plan.Name.ValueString(), r.input(plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not update model provider", err.Error())
		return
	}
	apply(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *modelProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteModelProvider(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		return
	}
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		resp.Diagnostics.AddError(
			"Model provider is still in use",
			"horsie refuses to delete a provider while a model still routes to it, because that would "+
				"take a session's model with it. Destroy the horsie_model resources first — Terraform "+
				"does that automatically if they reference this one.\n\n"+apiErr.Error(),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not delete model provider", err.Error())
	}
}

func (r *modelProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
