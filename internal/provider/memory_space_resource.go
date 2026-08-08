package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

var (
	_ resource.Resource                = (*memorySpaceResource)(nil)
	_ resource.ResourceWithConfigure   = (*memorySpaceResource)(nil)
	_ resource.ResourceWithImportState = (*memorySpaceResource)(nil)
)

type memorySpaceResource struct{ client *client.Client }

// NewMemorySpaceResource registers `horsie_memory_space`.
func NewMemorySpaceResource() resource.Resource { return &memorySpaceResource{} }

type memorySpaceModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	MemoryCount types.Int64  `tfsdk:"memory_count"`
}

func (r *memorySpaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_memory_space"
}

func (r *memorySpaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A named namespace for an agent's long-term memories.\n\n" +
			"Only the space is managed here. The memories inside it are written and rewritten " +
			"by the agent as it works, so managing them would mean a plan that shows drift every " +
			"time an agent thinks.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Slug: lowercase letters, digits, `.`, `_` and `-`, starting with a letter or digit. " +
					"Renaming is done in place and carries the space's memories across.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "What this space holds. Defaults to empty.",
			},
			"memory_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "How many memories the space currently holds. Changes as the agent works, so it is read-only.",
			},
		},
	}
}

func (r *memorySpaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func applySpace(m *memorySpaceModel, v *api.MemorySpaceView) {
	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.MemoryCount = types.Int64Value(int64(v.MemoryCount))
}

func (r *memorySpaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan memorySpaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := api.MemorySpaceCreateInput{Name: plan.Name.ValueString()}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		in.Description = &v
	}
	view, err := r.client.CreateMemorySpace(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Could not create memory space", err.Error())
		return
	}
	applySpace(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memorySpaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state memorySpaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetMemorySpace(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read memory space", err.Error())
		return
	}
	applySpace(&state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *memorySpaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state memorySpaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The update is addressed by the *old* name, and carries the new one in the
	// body — a rename is in place here, not a replace, so the memories survive.
	var in api.MemorySpaceUpdateInput
	if plan.Name.ValueString() != state.Name.ValueString() {
		v := plan.Name.ValueString()
		in.Name = &v
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		in.Description = &v
	}

	view, err := r.client.UpdateMemorySpace(ctx, state.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Could not update memory space", err.Error())
		return
	}
	applySpace(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memorySpaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state memorySpaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteMemorySpace(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete memory space", err.Error())
	}
}

func (r *memorySpaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
