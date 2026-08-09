package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
)

var (
	_ datasource.DataSource              = (*runtimeVendorsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*runtimeVendorsDataSource)(nil)
)

type runtimeVendorsDataSource struct{ client *client.Client }

// NewRuntimeVendorsDataSource registers `horsie_runtime_vendors`.
func NewRuntimeVendorsDataSource() datasource.DataSource { return &runtimeVendorsDataSource{} }

// runtimeVendorObjectType is the element type of the `vendors` list.
//
// Written by hand because the model below holds a types.List rather than a Go
// slice: every attribute of a data source is Computed, so the list carries an
// unknown before refresh and a slice cannot hold one. It is pinned to the
// schema by a test, since nothing else keeps the two in step.
var runtimeVendorObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":                  types.StringType,
	"is_default":            types.BoolType,
	"supports_provisioning": types.BoolType,
}}

type runtimeVendorsModel struct {
	Vendors types.List `tfsdk:"vendors"`
}

func (d *runtimeVendorsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runtime_vendors"
}

func (d *runtimeVendorsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every runtime vendor this server can start a session on.\n\n" +
			"The live roster rather than the stored configuration: `local`, vendors that dialled in " +
			"with `horsie connect`, and vendors configured as `horsie_runtime_vendor` all appear " +
			"here. It is the only way to reference a vendor Terraform did not create — a laptop " +
			"running `horsie connect` has no configuration to declare.",
		Attributes: map[string]schema.Attribute{
			"vendors": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The roster, in the order horsie reports it.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The name sessions and environments select this vendor by.",
						},
						"is_default": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether new sessions default to it. See `horsie_default_runtime_vendor`.",
						},
						"supports_provisioning": schema.BoolAttribute{
							Computed: true,
							MarkdownDescription: "Whether it builds a workspace it owns — cloning repos, installing " +
								"skill bundles, running provision steps. Only a vendor that does can back a " +
								"`horsie_environment`; one fixed to a user-owned directory, like the local " +
								"daemon, provisions nothing.",
						},
					},
				},
			},
		},
	}
}

func (d *runtimeVendorsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *runtimeVendorsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	settings, err := d.client.GetSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the runtime vendors", err.Error())
		return
	}

	elements := make([]attr.Value, 0, len(settings.Vendors))
	for _, v := range settings.Vendors {
		obj, diags := types.ObjectValue(runtimeVendorObjectType.AttrTypes, map[string]attr.Value{
			"name":                  types.StringValue(v.Name),
			"is_default":            types.BoolValue(v.IsDefault),
			"supports_provisioning": types.BoolValue(v.Capabilities.SupportsProvisioning),
		})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elements = append(elements, obj)
	}

	list, diags := types.ListValue(runtimeVendorObjectType, elements)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &runtimeVendorsModel{Vendors: list})...)
}
