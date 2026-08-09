package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
)

var (
	_ resource.Resource                = (*defaultRuntimeVendorResource)(nil)
	_ resource.ResourceWithConfigure   = (*defaultRuntimeVendorResource)(nil)
	_ resource.ResourceWithImportState = (*defaultRuntimeVendorResource)(nil)
)

type defaultRuntimeVendorResource struct{ client *client.Client }

// NewDefaultRuntimeVendorResource registers `horsie_default_runtime_vendor`.
func NewDefaultRuntimeVendorResource() resource.Resource { return &defaultRuntimeVendorResource{} }

type defaultRuntimeVendorModel struct {
	Vendor types.String `tfsdk:"vendor"`
}

func (r *defaultRuntimeVendorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_default_runtime_vendor"
}

func (r *defaultRuntimeVendorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Which runtime vendor new sessions target when they name none.\n\n" +
			"A server has exactly one, so this is a singleton. Declaring two of these against the " +
			"same server is not something Terraform can catch, and they will overwrite each other " +
			"on every apply.\n\n" +
			"Destroying this resource clears the preference and horsie falls back to `local`. " +
			"Because horsie reports `local` both when nothing is set and when `local` is set " +
			"deliberately, those two states cannot be told apart on read.",
		Attributes: map[string]schema.Attribute{
			"vendor": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "A runtime vendor name. Not validated against the live roster: " +
					"the agent answering to a name may connect long after the preference is set, and " +
					"refusing it here would make the setting unusable before its agent is running.",
			},
		},
	}
}

func (r *defaultRuntimeVendorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// set is the whole of Create and Update: there is one row, and writing it is
// the same operation whether or not Terraform has written it before.
func (r *defaultRuntimeVendorResource) set(ctx context.Context, m defaultRuntimeVendorModel) error {
	_, err := r.client.SetDefaultRuntimeVendor(ctx, m.Vendor.ValueString())
	return err
}

func (r *defaultRuntimeVendorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m defaultRuntimeVendorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, m); err != nil {
		resp.Diagnostics.AddError("Failed to set the default runtime vendor", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *defaultRuntimeVendorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m defaultRuntimeVendorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings, err := r.client.GetSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the default runtime vendor", err.Error())
		return
	}
	// Whatever the server says is the truth, including `local` — there is no
	// "unset" to distinguish, so there is nothing to remove from state here.
	m.Vendor = types.StringValue(settings.DefaultRuntimeVendor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *defaultRuntimeVendorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m defaultRuntimeVendorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, m); err != nil {
		resp.Diagnostics.AddError("Failed to set the default runtime vendor", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *defaultRuntimeVendorResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if _, err := r.client.ClearDefaultRuntimeVendor(ctx); err != nil {
		resp.Diagnostics.AddError("Failed to clear the default runtime vendor", err.Error())
	}
}

func (r *defaultRuntimeVendorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("vendor"), req, resp)
}
