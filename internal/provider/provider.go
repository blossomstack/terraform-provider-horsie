// Package provider implements the horsie Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
)

// Ensure the implementation satisfies the framework interface.
var _ provider.Provider = (*horsieProvider)(nil)

type horsieProvider struct {
	version string
}

// New returns the provider factory tfplugin needs.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &horsieProvider{version: version} }
}

func (p *horsieProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "horsie"
	resp.Version = p.version
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
}

func (p *horsieProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage a horsie server's configuration: model providers, model aliases, " +
			"and the presets sessions are created from.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL of the horsie server, e.g. `https://horsie.example.com`. " +
					"Falls back to the `HORSIE_ENDPOINT` environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "A horsie agent token. Create one with `POST /api/device/tokens`; " +
					"it is shown once and stored hashed, so it cannot be read back. " +
					"Falls back to the `HORSIE_TOKEN` environment variable.",
			},
		},
	}
}

func (p *horsieProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Environment variables are the fallback rather than the override: an
	// explicit value in HCL is the more specific statement of intent, and a
	// stray exported variable should not silently retarget an apply.
	endpoint := cfg.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("HORSIE_ENDPOINT")
	}
	token := cfg.Token.ValueString()
	if token == "" {
		token = os.Getenv("HORSIE_TOKEN")
	}

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing horsie endpoint",
			"Set the provider's `endpoint` argument or the HORSIE_ENDPOINT environment variable.",
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing horsie token",
			"Set the provider's `token` argument or the HORSIE_TOKEN environment variable. "+
				"Create a token with `POST /api/device/tokens`.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	c := client.New(endpoint, token)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *horsieProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewModelProviderResource,
		NewModelResource,
		NewMemorySpaceResource,
		NewAgentResource,
		NewEnvironmentResource,
	}
}

func (p *horsieProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
