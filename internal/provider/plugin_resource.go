package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                = (*pluginResource)(nil)
	_ resource.ResourceWithConfigure   = (*pluginResource)(nil)
	_ resource.ResourceWithImportState = (*pluginResource)(nil)
)

type pluginResource struct{ client *client.Client }

// NewPluginResource registers `horsie_plugin`.
func NewPluginResource() resource.Resource { return &pluginResource{} }

// catalogEntryModel mirrors one entry of what a bundle offers. Read-only: the
// catalogue is what the repo contains, not something Terraform chooses.
type catalogEntryModel struct {
	Kind         types.String `tfsdk:"kind"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	ArgumentHint types.String `tfsdk:"argument_hint"`
}

// catalogEntryType is the element type of `catalog`. It has to be spelled out
// because the attribute is computed: the plan carries it as unknown, and only a
// `types.List` can hold that — a `[]catalogEntryModel` field fails the apply
// with "Received unknown value, however the target type cannot handle unknown
// values" before the resource is ever created.
var catalogEntryType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"kind":          types.StringType,
	"name":          types.StringType,
	"description":   types.StringType,
	"argument_hint": types.StringType,
}}

type pluginModel struct {
	Name           types.String `tfsdk:"name"`
	SourceURL      types.String `tfsdk:"source_url"`
	SourceRef      types.String `tfsdk:"source_ref"`
	EnabledDefault types.Bool   `tfsdk:"enabled_default"`
	Description    types.String `tfsdk:"description"`
	Version        types.String `tfsdk:"version"`
	HasHooks       types.Bool   `tfsdk:"has_hooks"`
	ArtifactSize   types.Int64  `tfsdk:"artifact_size"`
	Catalog        types.List   `tfsdk:"catalog"`
}

func (r *pluginResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin"
}

func (r *pluginResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A plugin bundle installed from a git repository: its skills, commands, " +
			"agents and hooks.\n\n" +
			"horsie clones the repository once, at install, and keeps its own copy — so what a " +
			"session runs is the commit that was installed, not whatever the branch points at today. " +
			"Pin `source_ref` to a commit sha and the deployment is reproducible; point it at a " +
			"branch and Terraform will see no change even after the remote moves.",
		Attributes: map[string]schema.Attribute{
			"source_url": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Clone URL of the bundle repository. Changing it replaces the resource, " +
					"because horsie only re-clones a bundle from the source it was installed from.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"source_ref": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Branch, tag or commit sha to clone. Omit for the repository's default " +
					"branch. Changing it replaces the resource: horsie has no \"re-pin\" operation, so " +
					"moving to a new commit means uninstalling and installing again.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled_default": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Pre-select this bundle in the new-session picker. Defaults to false — " +
					"installing a bundle makes it available, not automatic.",
			},
			"name": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The bundle's canonical name, used in API paths and as the import id. " +
					"**The server assigns it** — from the repository's `plugin.json`, else the repo basename — " +
					"so it is discovered at install rather than chosen here.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The bundle's own description, from its manifest.",
			},
			"version": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Resolved version: the manifest's version, else the sha of the cloned " +
					"commit. This is what actually got installed, which is the useful thing to record when " +
					"`source_ref` is a branch.",
			},
			"has_hooks": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the bundle ships hooks horsie will run. Worth reviewing: a hook " +
					"runs as part of a session rather than being invoked by one.",
			},
			"artifact_size": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Size of the stored bundle in bytes.",
			},
			"catalog": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Everything the bundle offers, sorted by kind then name.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"kind": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "`command`, `skill` or `agent`.",
						},
						"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "What it is typed as, without the `/` or `@`."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Its one-line summary."},
						"argument_hint": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "`argument-hint`, shown beside the name. Commands only.",
						},
					},
				},
			},
		},
	}
}

func (r *pluginResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func applyPlugin(ctx context.Context, m *pluginModel, v *api.PluginView) {
	m.Name = types.StringValue(v.Name)
	m.SourceURL = types.StringValue(v.SourceURL)
	m.SourceRef = optString(v.SourceRef)
	m.EnabledDefault = types.BoolValue(v.EnabledDefault)
	m.Description = optString(v.Description)
	m.Version = optString(v.Version)
	m.HasHooks = types.BoolValue(v.HasHooks)
	m.ArtifactSize = types.Int64Value(int64(v.ArtifactSize))

	entries := make([]catalogEntryModel, 0, len(v.Catalog))
	for _, e := range v.Catalog {
		entries = append(entries, catalogEntryModel{
			Kind:         types.StringValue(e.Kind),
			Name:         types.StringValue(e.Name),
			Description:  types.StringValue(e.Description),
			ArgumentHint: optString(e.ArgumentHint),
		})
	}
	list, diags := types.ListValueFrom(ctx, catalogEntryType, entries)
	if diags.HasError() {
		// Only reachable if catalogEntryType and catalogEntryModel drift apart,
		// which is a provider bug and is what TestCatalogElementTypeMatchesTheSchema
		// guards; an empty list beats a half-built one.
		list = types.ListValueMust(catalogEntryType, nil)
	}
	m.Catalog = list
}

// installed unwraps the outcome, turning "that URL is a catalogue" into a
// diagnostic that says so. Both outcomes are a 201 with a row created, so a
// marketplace would otherwise look like a successful apply that installed
// nothing.
func installed(out *api.InstallOutcome) (*api.PluginView, error) {
	switch variant := out.Variant.(type) {
	case api.InstallOutcomeInstalled:
		return &variant.Value, nil
	case api.InstallOutcomeMarketplace:
		names := make([]string, 0, len(variant.Value.Plugins))
		for _, p := range variant.Value.Plugins {
			names = append(names, p.Name)
		}
		return nil, fmt.Errorf(
			"%s is a plugin marketplace offering %d bundles, not a bundle itself. "+
				"Point source_url at one of the repositories it lists instead: %s",
			variant.Value.SourceURL, variant.Value.PluginCount, strings.Join(names, ", "))
	default:
		return nil, fmt.Errorf("unsupported install outcome %T", out.Variant)
	}
}

func (r *pluginResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pluginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := api.PluginInstallInput{}
	url := plan.SourceURL.ValueString()
	in.SourceURL = &url
	if !plan.SourceRef.IsNull() {
		ref := plan.SourceRef.ValueString()
		in.SourceRef = &ref
	}

	outcome, err := r.client.InstallPlugin(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Could not install plugin", err.Error())
		return
	}
	view, err := installed(outcome)
	if err != nil {
		resp.Diagnostics.AddError("Not a plugin bundle", err.Error())
		return
	}

	// Install carries no enabled_default, so an explicit `true` is a second
	// call. Skipping it when it already matches keeps a default-shaped create
	// to one request.
	if !plan.EnabledDefault.IsNull() && !plan.EnabledDefault.IsUnknown() &&
		plan.EnabledDefault.ValueBool() != view.EnabledDefault {
		view, err = r.client.SetPluginDefault(ctx, view.Name, plan.EnabledDefault.ValueBool())
		if err != nil {
			resp.Diagnostics.AddError("Could not set enabled_default", err.Error())
			return
		}
	}

	applyPlugin(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pluginResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pluginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetPlugin(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read plugin", err.Error())
		return
	}
	applyPlugin(ctx, &state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever handles enabled_default: the source is what the bundle *is*,
// and both of its attributes replace the resource.
func (r *pluginResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state pluginModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.SetPluginDefault(ctx, state.Name.ValueString(), plan.EnabledDefault.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Could not update plugin", err.Error())
		return
	}
	applyPlugin(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pluginResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pluginModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeletePlugin(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete plugin", err.Error())
	}
}

// ImportState takes the bundle name — the id horsie knows it by, which for an
// import is the only thing the operator has. source_url and source_ref come
// back with the first Read.
func (r *pluginResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
