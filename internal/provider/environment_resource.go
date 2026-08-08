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
	_ resource.Resource                = (*environmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*environmentResource)(nil)
	_ resource.ResourceWithImportState = (*environmentResource)(nil)
)

type environmentResource struct{ client *client.Client }

// NewEnvironmentResource registers `horsie_environment`.
func NewEnvironmentResource() resource.Resource { return &environmentResource{} }

type envVarModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

type stepParamModel struct {
	Key   types.String `tfsdk:"key"`
	Value types.String `tfsdk:"value"`
}

type provisionStepModel struct {
	Name types.String     `tfsdk:"name"`
	Uses types.String     `tfsdk:"uses"`
	With []stepParamModel `tfsdk:"with"`
}

type environmentModel struct {
	Name        types.String         `tfsdk:"name"`
	Description types.String         `tfsdk:"description"`
	Vendor      types.String         `tfsdk:"vendor"`
	Repos       []repoModel          `tfsdk:"repos"`
	EnvVars     []envVarModel        `tfsdk:"env_var"`
	Provision   []provisionStepModel `tfsdk:"provision"`
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A reusable runtime + repos bundle.\n\n" +
			"**Experimental.** horsie does not consume environments yet — sessions, agent presets " +
			"and routines do not reference one, and the `provision` steps are inert. The resource " +
			"exists so the definition can be written down now; expect its consumers to arrive later.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug used in API paths. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "What this environment is for. Defaults to empty.",
			},
			"vendor": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Runtime vendor name. Required, and never `local`: environments only " +
					"target vendor-managed, provisionable runtimes.",
			},
		},
		Blocks: map[string]schema.Block{
			"repos": schema.ListNestedBlock{
				MarkdownDescription: "Repositories cloned into the runtime workspace at provision time.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"url":     schema.StringAttribute{Required: true, MarkdownDescription: "HTTPS clone URL."},
						"git_ref": schema.StringAttribute{Optional: true, MarkdownDescription: "Branch, tag or commit. Omit for the default branch."},
						"dir":     schema.StringAttribute{Optional: true, MarkdownDescription: "Directory under the workspace. Omit for the repo basename."},
					},
				},
			},
			"env_var": schema.ListNestedBlock{
				MarkdownDescription: "Plain-text, non-sensitive environment variables for the runtime. " +
					"Secrets are a separate future concept — do not put one here, it is stored and returned in clear.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name":  schema.StringAttribute{Required: true},
						"value": schema.StringAttribute{Required: true},
					},
				},
			},
			"provision": schema.ListNestedBlock{
				MarkdownDescription: "Setup steps the runtime executes before its message loop. Inert today.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{Required: true, MarkdownDescription: "Display label, e.g. `checkout horsie`."},
						"uses": schema.StringAttribute{Required: true, MarkdownDescription: "Step kind, e.g. `git_checkout`."},
					},
					Blocks: map[string]schema.Block{
						"with": schema.ListNestedBlock{
							MarkdownDescription: "Key/value parameters, interpreted per `uses`.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"key":   schema.StringAttribute{Required: true},
									"value": schema.StringAttribute{Required: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *environmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *environmentResource) input(m environmentModel) api.EnvironmentInput {
	in := api.EnvironmentInput{Name: m.Name.ValueString(), Vendor: m.Vendor.ValueString()}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		v := m.Description.ValueString()
		in.Description = &v
	}
	if len(m.Repos) > 0 {
		repos := make([]api.RepoConfig, 0, len(m.Repos))
		for _, rc := range m.Repos {
			one := api.RepoConfig{URL: rc.URL.ValueString()}
			if !rc.GitRef.IsNull() {
				v := rc.GitRef.ValueString()
				one.GitRef = &v
			}
			if !rc.Dir.IsNull() {
				v := rc.Dir.ValueString()
				one.Dir = &v
			}
			repos = append(repos, one)
		}
		in.Repos = &repos
	}
	if len(m.EnvVars) > 0 {
		vars := make([]api.EnvVar, 0, len(m.EnvVars))
		for _, e := range m.EnvVars {
			vars = append(vars, api.EnvVar{Name: e.Name.ValueString(), Value: e.Value.ValueString()})
		}
		in.EnvVars = &vars
	}
	if len(m.Provision) > 0 {
		steps := make([]api.ProvisionStep, 0, len(m.Provision))
		for _, s := range m.Provision {
			with := make([]api.StepParam, 0, len(s.With))
			for _, p := range s.With {
				with = append(with, api.StepParam{Key: p.Key.ValueString(), Value: p.Value.ValueString()})
			}
			steps = append(steps, api.ProvisionStep{
				Name: s.Name.ValueString(),
				Uses: s.Uses.ValueString(),
				With: with,
			})
		}
		in.Provision = &steps
	}
	return in
}

func applyEnvironment(m *environmentModel, v *api.EnvironmentView) {
	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.Vendor = types.StringValue(v.Vendor)

	// Empty reads back as no block at all, so an environment with nothing set
	// does not plan as a permanent diff.
	m.Repos = nil
	for _, rc := range v.Repos {
		m.Repos = append(m.Repos, repoModel{
			URL:    types.StringValue(rc.URL),
			GitRef: optString(rc.GitRef),
			Dir:    optString(rc.Dir),
		})
	}
	m.EnvVars = nil
	for _, e := range v.EnvVars {
		m.EnvVars = append(m.EnvVars, envVarModel{
			Name:  types.StringValue(e.Name),
			Value: types.StringValue(e.Value),
		})
	}
	m.Provision = nil
	for _, s := range v.Provision {
		step := provisionStepModel{
			Name: types.StringValue(s.Name),
			Uses: types.StringValue(s.Uses),
		}
		for _, p := range s.With {
			step.With = append(step.With, stepParamModel{
				Key:   types.StringValue(p.Key),
				Value: types.StringValue(p.Value),
			})
		}
		m.Provision = append(m.Provision, step)
	}
}

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.CreateEnvironment(ctx, r.input(plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not create environment", err.Error())
		return
	}
	applyEnvironment(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetEnvironment(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read environment", err.Error())
		return
	}
	applyEnvironment(&state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan environmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.ReplaceEnvironment(ctx, plan.Name.ValueString(), r.input(plan))
	if err != nil {
		resp.Diagnostics.AddError("Could not update environment", err.Error())
		return
	}
	applyEnvironment(&plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *environmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state environmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteEnvironment(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete environment", err.Error())
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
