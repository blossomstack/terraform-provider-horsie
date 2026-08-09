package provider

import (
	"context"
	"errors"
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
	_ resource.Resource                   = (*runtimeVendorResource)(nil)
	_ resource.ResourceWithConfigure      = (*runtimeVendorResource)(nil)
	_ resource.ResourceWithImportState    = (*runtimeVendorResource)(nil)
	_ resource.ResourceWithValidateConfig = (*runtimeVendorResource)(nil)
)

type runtimeVendorResource struct{ client *client.Client }

// NewRuntimeVendorResource registers `horsie_runtime_vendor`.
func NewRuntimeVendorResource() resource.Resource { return &runtimeVendorResource{} }

// flySettingsModel is HCL's shape for a Fly Machines vendor.
//
// Every field is required. horsie's wire types carry no optionals here and the
// server applies no defaults on write, so a default offered here would be a
// third hardcoded copy of constants that already live in horsie's `Default`
// impl and in its settings form — and one that keeps writing stale values if
// either of those ever changes. Sizing that provisions real machines is worth
// saying out loud.
type flySettingsModel struct {
	App           types.String `tfsdk:"app"`
	Image         types.String `tfsdk:"image"`
	Region        types.String `tfsdk:"region"`
	WorkspaceRoot types.String `tfsdk:"workspace_root"`
	CallbackURL   types.String `tfsdk:"callback_url"`
	Volumes       types.Bool   `tfsdk:"volumes"`
	CPUKind       types.String `tfsdk:"cpu_kind"`
	CPUs          types.Int64  `tfsdk:"cpus"`
	MemoryMB      types.Int64  `tfsdk:"memory_mb"`
	VolumeSizeGB  types.Int64  `tfsdk:"volume_size_gb"`
}

// velosSettingsModel is HCL's shape for a velos vendor.
type velosSettingsModel struct {
	ServerURL     types.String `tfsdk:"server_url"`
	Image         types.String `tfsdk:"image"`
	RuntimeBin    types.String `tfsdk:"runtime_bin"`
	WorkspaceRoot types.String `tfsdk:"workspace_root"`
	CallbackURL   types.String `tfsdk:"callback_url"`
	CPU           types.Int64  `tfsdk:"cpu"`
	MemoryMB      types.Int64  `tfsdk:"memory_mb"`
}

// runtimeVendorModel flattens horsie's `RuntimeVendorSettings` union into one
// block per kind. The block that is present *is* the kind, so there is no
// discriminator to keep in step with it, and a third substrate is a third block
// rather than a new resource type.
type runtimeVendorModel struct {
	Name          types.String        `tfsdk:"name"`
	Credential    types.String        `tfsdk:"credential"`
	HasCredential types.Bool          `tfsdk:"has_credential"`
	Fly           *flySettingsModel   `tfsdk:"fly"`
	Velos         *velosSettingsModel `tfsdk:"velos"`
}

func (r *runtimeVendorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runtime_vendor"
}

func (r *runtimeVendorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A cloud runtime vendor the server builds itself: the substrate sessions " +
			"get their sandbox from.\n\n" +
			"Only vendors horsie provisions belong here. One that dials in with `horsie connect` " +
			"announces itself over a websocket and disappears when its link drops, so there is " +
			"nothing to declare about it — read those from `data.horsie_runtime_vendors` instead. " +
			"Names are shared across both kinds, and horsie refuses a configured vendor the name of " +
			"a connected one.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name sessions and environments select this vendor by. " +
					"Changing it replaces the resource: horsie refuses a body whose name disagrees " +
					"with the path rather than renaming a row, because a silent move would strand " +
					"every environment pointing at the old name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"credential": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "The vendor API token — a Fly API token, or a velos bearer. " +
					"Omit to leave a stored token untouched, which is what lets a caller that can " +
					"never read the secret manage the rest of the vendor. A velos deployment that " +
					"runs without auth is configured with `\"\"`; the field cannot be left out " +
					"entirely when creating, only emptied.",
			},
			"has_credential": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether a token is stored. The token itself is never returned — " +
					"a settings page that could read one back would turn every session cookie into a " +
					"way to steal it.",
			},
		},
		Blocks: map[string]schema.Block{
			"fly": schema.SingleNestedBlock{
				MarkdownDescription: "Machines on [Fly](https://fly.io). Exactly one of `fly` or `velos` " +
					"must be given.",
				Attributes: map[string]schema.Attribute{
					"app": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "The Fly app machines are created in. It must already exist — " +
							"horsie creates machines, not apps.",
					},
					"image": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "OCI image with `horsie-runtime` baked in.",
					},
					"region": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Fly region code, e.g. `iad`.",
					},
					"workspace_root": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Where inside the machine workspaces are allocated, e.g. `/workspaces`.",
					},
					"callback_url": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "The `wss://` URL a machine reaches this server on, **including " +
							"the connect path** — `wss://horsie.example.com/api/runtime/connect`. horsie " +
							"refuses a URL with no path rather than completing one, so that what is stored " +
							"is always what was written. An address that only resolves on the server itself " +
							"(`localhost`, `127.0.0.1`, `::1`, anything under `.localhost`) is refused too: " +
							"inside a machine those names mean the machine.",
					},
					"volumes": schema.BoolAttribute{
						Required: true,
						MarkdownDescription: "Give each runtime a volume, so a stopped one keeps its " +
							"workspace. Without it a hibernated session cannot be resumed.",
					},
					"cpu_kind": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`shared` or `performance`.",
					},
					"cpus": schema.Int64Attribute{
						Required:            true,
						MarkdownDescription: "vCPUs per machine.",
					},
					"memory_mb": schema.Int64Attribute{
						Required:            true,
						MarkdownDescription: "Memory per machine, in MB.",
					},
					"volume_size_gb": schema.Int64Attribute{
						Required: true,
						MarkdownDescription: "Volume size in GB. Required even when `volumes = false`, " +
							"where horsie ignores it — the settings horsie stores have no optionals.",
					},
				},
			},
			"velos": schema.SingleNestedBlock{
				MarkdownDescription: "Containers on a velos deployment. Exactly one of `fly` or `velos` " +
					"must be given.",
				Attributes: map[string]schema.Attribute{
					"server_url": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "The velos server root, e.g. `http://velos:8080`.",
					},
					"image": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "OCI image bundling `horsie-runtime`, built without the " +
							"sandbox feature — the container is already the isolation boundary.",
					},
					"runtime_bin": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Path to `horsie-runtime` inside the image.",
					},
					"workspace_root": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "Where inside the container workspaces are allocated.",
					},
					"callback_url": schema.StringAttribute{
						Required: true,
						MarkdownDescription: "The `ws://` URL a container reaches this server on **from " +
							"velos's container network**, including the connect path — not necessarily " +
							"the address a browser uses.",
					},
					"cpu": schema.Int64Attribute{
						Required:            true,
						MarkdownDescription: "vCPUs per container.",
					},
					"memory_mb": schema.Int64Attribute{
						Required:            true,
						MarkdownDescription: "Memory per container, in MB.",
					},
				},
			},
		},
	}
}

func (r *runtimeVendorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig turns the union's one rule into a plan-time diagnostic.
//
// Without it, a vendor with no block fails inside the generated marshaller with
// `fluorite: RuntimeVendorSettings has no variant set` partway through an
// apply, which names nothing anyone can act on.
func (r *runtimeVendorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m runtimeVendorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	switch {
	case m.Fly == nil && m.Velos == nil:
		resp.Diagnostics.AddError("Missing vendor kind",
			"A runtime vendor needs exactly one `fly` or `velos` block. The block you write is the "+
				"kind of vendor horsie builds, so there is nothing for it to be without one.")
	case m.Fly != nil && m.Velos != nil:
		resp.Diagnostics.AddAttributeError(path.Root("velos"), "Two vendor kinds",
			"A runtime vendor has exactly one kind, so `fly` and `velos` cannot both be given. "+
				"Declare a second `horsie_runtime_vendor` for the other substrate.")
	}
}

// settings builds the wire union from whichever block is set.
func (m runtimeVendorModel) settings() (api.RuntimeVendorSettings, error) {
	switch {
	case m.Fly != nil && m.Velos != nil:
		return api.RuntimeVendorSettings{}, errors.New("a runtime vendor has exactly one kind, but both `fly` and `velos` are set")
	case m.Fly != nil:
		return api.RuntimeVendorSettings{Variant: api.RuntimeVendorSettingsFly{
			Value: api.FlyVendorSettings{
				App:           m.Fly.App.ValueString(),
				Image:         m.Fly.Image.ValueString(),
				Region:        m.Fly.Region.ValueString(),
				WorkspaceRoot: m.Fly.WorkspaceRoot.ValueString(),
				CallbackURL:   m.Fly.CallbackURL.ValueString(),
				Volumes:       m.Fly.Volumes.ValueBool(),
				CPUKind:       m.Fly.CPUKind.ValueString(),
				Cpus:          uint32(m.Fly.CPUs.ValueInt64()),
				MemoryMb:      uint32(m.Fly.MemoryMB.ValueInt64()),
				VolumeSizeGb:  uint32(m.Fly.VolumeSizeGB.ValueInt64()),
			},
		}}, nil
	case m.Velos != nil:
		return api.RuntimeVendorSettings{Variant: api.RuntimeVendorSettingsVelos{
			Value: api.VelosVendorSettings{
				ServerURL:     m.Velos.ServerURL.ValueString(),
				Image:         m.Velos.Image.ValueString(),
				RuntimeBin:    m.Velos.RuntimeBin.ValueString(),
				WorkspaceRoot: m.Velos.WorkspaceRoot.ValueString(),
				CallbackURL:   m.Velos.CallbackURL.ValueString(),
				CPU:           uint32(m.Velos.CPU.ValueInt64()),
				MemoryMb:      uint32(m.Velos.MemoryMB.ValueInt64()),
			},
		}}, nil
	default:
		return api.RuntimeVendorSettings{}, errors.New("a runtime vendor needs exactly one `fly` or `velos` block")
	}
}

// applyView writes a server view over the model, replacing the settings block
// rather than merging into it: a vendor whose kind changed underneath Terraform
// has to read back as the kind it actually is.
//
// Credential is untouched — horsie never returns one, so whatever the
// configuration said about it stands.
func (m *runtimeVendorModel) applyView(v api.RuntimeVendorConfigView) {
	m.Name = types.StringValue(v.Name)
	m.HasCredential = types.BoolValue(v.HasCredential)
	m.Fly, m.Velos = nil, nil
	switch s := v.Settings.Variant.(type) {
	case api.RuntimeVendorSettingsFly:
		m.Fly = &flySettingsModel{
			App:           types.StringValue(s.Value.App),
			Image:         types.StringValue(s.Value.Image),
			Region:        types.StringValue(s.Value.Region),
			WorkspaceRoot: types.StringValue(s.Value.WorkspaceRoot),
			CallbackURL:   types.StringValue(s.Value.CallbackURL),
			Volumes:       types.BoolValue(s.Value.Volumes),
			CPUKind:       types.StringValue(s.Value.CPUKind),
			CPUs:          types.Int64Value(int64(s.Value.Cpus)),
			MemoryMB:      types.Int64Value(int64(s.Value.MemoryMb)),
			VolumeSizeGB:  types.Int64Value(int64(s.Value.VolumeSizeGb)),
		}
	case api.RuntimeVendorSettingsVelos:
		m.Velos = &velosSettingsModel{
			ServerURL:     types.StringValue(s.Value.ServerURL),
			Image:         types.StringValue(s.Value.Image),
			RuntimeBin:    types.StringValue(s.Value.RuntimeBin),
			WorkspaceRoot: types.StringValue(s.Value.WorkspaceRoot),
			CallbackURL:   types.StringValue(s.Value.CallbackURL),
			CPU:           types.Int64Value(int64(s.Value.CPU)),
			MemoryMB:      types.Int64Value(int64(s.Value.MemoryMb)),
		}
	}
}

// input builds the request body. A null credential is omitted, which is horsie's
// "keep the stored token"; an empty string is a deliberate no-auth velos vendor
// and is sent.
func (m runtimeVendorModel) input() (api.RuntimeVendorConfigInput, error) {
	settings, err := m.settings()
	if err != nil {
		return api.RuntimeVendorConfigInput{}, err
	}
	in := api.RuntimeVendorConfigInput{Name: m.Name.ValueString(), Settings: settings}
	if !m.Credential.IsNull() && !m.Credential.IsUnknown() {
		v := m.Credential.ValueString()
		in.Credential = &v
	}
	return in, nil
}

func (r *runtimeVendorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m runtimeVendorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Checked here rather than left to horsie's 422, which arrives partway
	// through an apply. It checks for *set*, not for non-empty: a velos
	// deployment without auth is a supported configuration.
	if m.Credential.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("credential"), "Missing credential",
			"A new runtime vendor needs a credential. Use `credential = \"\"` for a velos "+
				"deployment that runs without auth.")
		return
	}
	in, err := m.input()
	if err != nil {
		resp.Diagnostics.AddError("Invalid runtime vendor", err.Error())
		return
	}
	view, err := r.client.PutRuntimeVendor(ctx, m.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create the runtime vendor", err.Error())
		return
	}
	m.applyView(*view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *runtimeVendorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m runtimeVendorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetRuntimeVendor(ctx, m.Name.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read the runtime vendor", err.Error())
		return
	}
	m.applyView(*view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *runtimeVendorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m runtimeVendorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := m.input()
	if err != nil {
		resp.Diagnostics.AddError("Invalid runtime vendor", err.Error())
		return
	}
	view, err := r.client.PutRuntimeVendor(ctx, m.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update the runtime vendor", err.Error())
		return
	}
	m.applyView(*view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *runtimeVendorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m runtimeVendorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A vendor already gone is the state being asked for.
	if err := r.client.DeleteRuntimeVendor(ctx, m.Name.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete the runtime vendor", err.Error())
	}
}

func (r *runtimeVendorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
