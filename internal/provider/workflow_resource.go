package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/blossomstack/terraform-provider-horsie/internal/client"
	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

var (
	_ resource.Resource                   = (*workflowResource)(nil)
	_ resource.ResourceWithConfigure      = (*workflowResource)(nil)
	_ resource.ResourceWithImportState    = (*workflowResource)(nil)
	_ resource.ResourceWithValidateConfig = (*workflowResource)(nil)
)

type workflowResource struct{ client *client.Client }

// NewWorkflowResource registers `horsie_workflow`.
func NewWorkflowResource() resource.Resource { return &workflowResource{} }

type workflowTransitionModel struct {
	To types.String `tfsdk:"to"`
	// The two operators of the wire union, flattened. Exactly one may be set;
	// neither is the catch-all.
	WhenOutcomeIn    types.List `tfsdk:"when_outcome_in"`
	WhenOutcomeNotIn types.List `tfsdk:"when_outcome_not_in"`
}

type workflowOutcomeModel struct {
	Value       types.String `tfsdk:"value"`
	Description types.String `tfsdk:"description"`
}

type workflowFieldModel struct {
	Name        types.String `tfsdk:"name"`
	Kind        types.String `tfsdk:"kind"`
	Description types.String `tfsdk:"description"`
	Required    types.Bool   `tfsdk:"required"`
}

type workflowStepModel struct {
	Name          types.String              `tfsdk:"name"`
	Agent         types.String              `tfsdk:"agent"`
	Prompt        types.String              `tfsdk:"prompt"`
	Interactive   types.Bool                `tfsdk:"interactive"`
	MaxIterations types.Int64               `tfsdk:"max_iterations"`
	MaxRetries    types.Int64               `tfsdk:"max_retries"`
	Outcomes      []workflowOutcomeModel    `tfsdk:"outcome"`
	Fields        []workflowFieldModel      `tfsdk:"result_field"`
	Transitions   []workflowTransitionModel `tfsdk:"transition"`
}

type workflowModel struct {
	Name        types.String        `tfsdk:"name"`
	Description types.String        `tfsdk:"description"`
	Start       types.String        `tfsdk:"start"`
	MaxSteps    types.Int64         `tfsdk:"max_steps"`
	Steps       []workflowStepModel `tfsdk:"step"`
	CreatedAt   types.String        `tfsdk:"created_at"`
	UpdatedAt   types.String        `tfsdk:"updated_at"`
}

func (r *workflowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow"
}

func (r *workflowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A named graph of steps, each an agent preset with a fixed prompt, wired " +
			"together by the outcome each step reports.\n\n" +
			"A definition is only the graph. Where a run happens is a property of the invocation, not " +
			"of the saved configuration, so nothing here names a runtime or a checkout — a step's " +
			"preset supplies the model, MCP servers and memory spaces, and the run supplies the rest.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug used in API paths. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "What this workflow is for. Defaults to empty.",
			},
			"start": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the step every run begins at. It must be one of the `step` blocks.",
			},
			"max_steps": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Most step executions one run may perform before it is failed; at least 1. " +
					"Omit for the server's default.\n\n" +
					"The only thing bounding a graph whose loop condition never flips, which is why it lives " +
					"on the definition rather than on the run: the budget is a property of the graph's shape, " +
					"and a workflow that legitimately loops twenty times knows that about itself.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unix epoch seconds.",
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unix epoch seconds.",
			},
		},
		Blocks: map[string]schema.Block{
			"step": schema.ListNestedBlock{
				MarkdownDescription: "One node of the graph. At least one is required, and every " +
					"`transition.to` must name one.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Step name, unique within the workflow, referenced by `start` and by transitions.",
						},
						"agent": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "Name of the `horsie_agent` preset this step runs as. Reference the " +
								"resource's `name` so Terraform orders the two correctly.",
						},
						"prompt": schema.StringAttribute{
							Required: true,
							MarkdownDescription: "The step's instruction. Whatever the step is handed — the run's input " +
								"for the start step, the previous step's result for every other — is appended below it " +
								"under a header, so the prompt says what to do rather than restating the input.",
						},
						"interactive": schema.BoolAttribute{
							Optional: true,
							MarkdownDescription: "Whether this step may stop and ask the person a question. Omit and the " +
								"step has no `ask_user` tool at all, so it must decide for itself.",
						},
						"max_iterations": schema.Int64Attribute{
							Optional:            true,
							MarkdownDescription: "Cap on agent-loop iterations for this step.",
						},
						"max_retries": schema.Int64Attribute{
							Optional:            true,
							MarkdownDescription: "Retry budget for transient provider errors within this step.",
						},
					},
					Blocks: map[string]schema.Block{
						"outcome": schema.ListNestedBlock{
							MarkdownDescription: "One value this step's `outcome` may take. Write none and the step " +
								"reports `success` or `failure`.\n\n" +
								"A step finishes by submitting a result carrying an `outcome` and a written " +
								"`description`; transitions read the outcome and nothing else.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"value": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "The value itself, as a transition names it.",
									},
									"description": schema.StringAttribute{
										Required: true,
										MarkdownDescription: "What choosing this outcome means. Not decoration: it is what the " +
											"model reads to pick between the values, and the only thing standing between " +
											"\"failure\" meaning *the work failed* and meaning *I could not finish*.",
									},
								},
							},
						},
						"result_field": schema.ListNestedBlock{
							MarkdownDescription: "One extra field this step's result carries, beyond the `outcome` and " +
								"`description` every result already has. Transitions cannot read these — they are " +
								"for the next step, which is handed them under the description.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										Required: true,
										MarkdownDescription: "Field name. `outcome` and `description` are taken — they are the two " +
											"fields every result carries.",
									},
									"kind": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "One of `String`, `Number`, `Boolean` or `StringList`.",
										Validators: []validator.String{stringvalidator.OneOf(
											string(api.StepFieldTypeString),
											string(api.StepFieldTypeNumber),
											string(api.StepFieldTypeBoolean),
											string(api.StepFieldTypeStringList),
										)},
									},
									"description": schema.StringAttribute{
										Required: true,
										MarkdownDescription: "What the field holds. Required: an undocumented field is one the " +
											"model fills in by guessing.",
									},
									"required": schema.BoolAttribute{
										Optional:            true,
										MarkdownDescription: "Whether the step must always supply it. Omit for optional.",
									},
								},
							},
						},
						"transition": schema.ListNestedBlock{
							MarkdownDescription: "A directed edge out of this step. **Order matters**: transitions are " +
								"tried in the order written and the first match wins, so put the catch-all last. " +
								"A step whose transitions all fail to match ends the run, carrying its result as the run's.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"to": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "Name of the step to go to.",
									},
									"when_outcome_in": schema.ListAttribute{
										Optional:    true,
										ElementType: types.StringType,
										MarkdownDescription: "Take this edge when the step's outcome is one of these. Every value " +
											"must be one the step declares — horsie refuses the workflow otherwise, because " +
											"at run time a filter that matches nothing is indistinguishable from a step that " +
											"meant to end the graph.",
									},
									"when_outcome_not_in": schema.ListAttribute{
										Optional:    true,
										ElementType: types.StringType,
										MarkdownDescription: "Take this edge when the step's outcome is none of these. The same " +
											"rule applies: every value must be one the step declares.",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *workflowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig turns the outcome filter's rules into plan-time diagnostics.
//
// The wire form is a union, so a transition naming both operators cannot be
// marshalled at all — without this it fails partway through an apply with
// `fluorite: OutcomeFilter has no variant set` or a silently dropped operator,
// neither of which names the transition at fault.
func (r *workflowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m workflowModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for si, s := range m.Steps {
		for ti, t := range s.Transitions {
			at := path.Root("step").AtListIndex(si).AtName("transition").AtListIndex(ti)
			in, notIn := !t.WhenOutcomeIn.IsNull(), !t.WhenOutcomeNotIn.IsNull()
			if in && notIn {
				resp.Diagnostics.AddAttributeError(at, "Two outcome filters",
					"A transition is gated one way or the other, so `when_outcome_in` and "+
						"`when_outcome_not_in` cannot both be given. Write a second `transition` "+
						"block if you need both edges.")
				continue
			}
			// An empty list is not a catch-all — horsie refuses a filter that
			// names no outcomes, because it can never match. Omit the argument
			// entirely for the catch-all.
			for _, l := range []types.List{t.WhenOutcomeIn, t.WhenOutcomeNotIn} {
				if !l.IsNull() && !l.IsUnknown() && len(l.Elements()) == 0 {
					resp.Diagnostics.AddAttributeError(at, "Outcome filter names no outcomes",
						"An empty filter can never match, which is not the same as no filter. "+
							"Omit both `when_outcome_in` and `when_outcome_not_in` for the catch-all.")
				}
			}
		}
	}
}

// when builds the wire union from whichever operator is set, or nil for a
// catch-all.
func (t workflowTransitionModel) when(ctx context.Context) (*api.OutcomeFilter, error) {
	in, notIn := stringsFromList(ctx, t.WhenOutcomeIn), stringsFromList(ctx, t.WhenOutcomeNotIn)
	switch {
	case in != nil && notIn != nil:
		return nil, fmt.Errorf("transition to %q sets both when_outcome_in and when_outcome_not_in, but an edge is gated one way or the other", t.To.ValueString())
	case in != nil:
		return &api.OutcomeFilter{Variant: api.OutcomeFilterIn{Value: api.OutcomeIn{Values: *in}}}, nil
	case notIn != nil:
		return &api.OutcomeFilter{Variant: api.OutcomeFilterNotIn{Value: api.OutcomeNotIn{Values: *notIn}}}, nil
	default:
		return nil, nil
	}
}

// toWorkflowInput builds the request body.
func toWorkflowInput(ctx context.Context, m workflowModel) (api.WorkflowInput, error) {
	in := api.WorkflowInput{
		Name:  m.Name.ValueString(),
		Start: m.Start.ValueString(),
		Steps: make([]api.WorkflowStepDef, 0, len(m.Steps)),
	}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		v := m.Description.ValueString()
		in.Description = &v
	}
	if !m.MaxSteps.IsNull() && !m.MaxSteps.IsUnknown() {
		v := uint32(m.MaxSteps.ValueInt64())
		in.MaxSteps = &v
	}

	for _, s := range m.Steps {
		step := api.WorkflowStepDef{
			Name:   s.Name.ValueString(),
			Agent:  s.Agent.ValueString(),
			Prompt: s.Prompt.ValueString(),
		}
		if !s.Interactive.IsNull() {
			v := s.Interactive.ValueBool()
			step.Interactive = &v
		}
		if !s.MaxIterations.IsNull() {
			v := uint32(s.MaxIterations.ValueInt64())
			step.MaxIterations = &v
		}
		if !s.MaxRetries.IsNull() {
			v := uint32(s.MaxRetries.ValueInt64())
			step.MaxRetries = &v
		}
		// Absent and empty mean the same thing for every one of these lists — a
		// step declaring no outcomes reports success/failure, and a terminal step
		// writes no transition blocks at all. Send absent, which is what the view
		// answers with and therefore what keeps a second plan empty.
		if len(s.Outcomes) > 0 {
			outcomes := make([]api.StepOutcome, 0, len(s.Outcomes))
			for _, o := range s.Outcomes {
				outcomes = append(outcomes, api.StepOutcome{
					Value:       o.Value.ValueString(),
					Description: o.Description.ValueString(),
				})
			}
			step.Outcomes = &outcomes
		}
		if len(s.Fields) > 0 {
			fields := make([]api.StepField, 0, len(s.Fields))
			for _, f := range s.Fields {
				field := api.StepField{
					Name:        f.Name.ValueString(),
					Kind:        api.StepFieldType(f.Kind.ValueString()),
					Description: f.Description.ValueString(),
				}
				if !f.Required.IsNull() {
					v := f.Required.ValueBool()
					field.Required = &v
				}
				fields = append(fields, field)
			}
			step.Fields = &fields
		}
		if len(s.Transitions) > 0 {
			edges := make([]api.WorkflowTransition, 0, len(s.Transitions))
			for _, t := range s.Transitions {
				filter, err := t.when(ctx)
				if err != nil {
					return in, fmt.Errorf("step %q: %w", step.Name, err)
				}
				edges = append(edges, api.WorkflowTransition{To: t.To.ValueString(), When: filter})
			}
			step.Transitions = &edges
		}
		in.Steps = append(in.Steps, step)
	}
	return in, nil
}

// whenModel splits the wire union back into the two flat arguments.
func whenModel(ctx context.Context, f *api.OutcomeFilter) (types.List, types.List) {
	null := types.ListNull(types.StringType)
	if f == nil {
		return null, null
	}
	switch v := f.Variant.(type) {
	case api.OutcomeFilterIn:
		return listFromStrings(ctx, v.Value.Values), null
	case api.OutcomeFilterNotIn:
		return null, listFromStrings(ctx, v.Value.Values)
	default:
		return null, null
	}
}

func applyWorkflow(ctx context.Context, m *workflowModel, v *api.WorkflowView) {
	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.Start = types.StringValue(v.Start)
	m.MaxSteps = optInt64(v.MaxSteps)
	m.CreatedAt = types.StringValue(v.CreatedAt)
	m.UpdatedAt = types.StringValue(v.UpdatedAt)

	m.Steps = nil
	for _, s := range v.Steps {
		step := workflowStepModel{
			Name:          types.StringValue(s.Name),
			Agent:         types.StringValue(s.Agent),
			Prompt:        types.StringValue(s.Prompt),
			Interactive:   optBool(s.Interactive),
			MaxIterations: optInt64(s.MaxIterations),
			MaxRetries:    optInt64(s.MaxRetries),
		}
		for _, o := range deref(s.Outcomes) {
			step.Outcomes = append(step.Outcomes, workflowOutcomeModel{
				Value:       types.StringValue(o.Value),
				Description: types.StringValue(o.Description),
			})
		}
		for _, f := range deref(s.Fields) {
			step.Fields = append(step.Fields, workflowFieldModel{
				Name:        types.StringValue(f.Name),
				Kind:        types.StringValue(string(f.Kind)),
				Description: types.StringValue(f.Description),
				Required:    optBool(f.Required),
			})
		}
		for _, t := range deref(s.Transitions) {
			in, notIn := whenModel(ctx, t.When)
			step.Transitions = append(step.Transitions, workflowTransitionModel{
				To:               types.StringValue(t.To),
				WhenOutcomeIn:    in,
				WhenOutcomeNotIn: notIn,
			})
		}
		m.Steps = append(m.Steps, step)
	}
}

func (r *workflowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := toWorkflowInput(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid workflow", err.Error())
		return
	}
	view, err := r.client.CreateWorkflow(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Could not create workflow", err.Error())
		return
	}
	applyWorkflow(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	view, err := r.client.GetWorkflow(ctx, state.Name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Could not read workflow", err.Error())
		return
	}
	applyWorkflow(ctx, &state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workflowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := toWorkflowInput(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid workflow", err.Error())
		return
	}
	view, err := r.client.ReplaceWorkflow(ctx, plan.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Could not update workflow", err.Error())
		return
	}
	applyWorkflow(ctx, &plan, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workflowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workflowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteWorkflow(ctx, state.Name.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Could not delete workflow", err.Error())
	}
}

func (r *workflowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
