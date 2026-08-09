package provider

import (
	"bytes"
	"context"
	"encoding/json"
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
	_ resource.Resource                = (*workflowResource)(nil)
	_ resource.ResourceWithConfigure   = (*workflowResource)(nil)
	_ resource.ResourceWithImportState = (*workflowResource)(nil)
)

type workflowResource struct{ client *client.Client }

// NewWorkflowResource registers `horsie_workflow`.
func NewWorkflowResource() resource.Resource { return &workflowResource{} }

type workflowTransitionModel struct {
	To        types.String `tfsdk:"to"`
	Condition types.String `tfsdk:"condition"`
}

type workflowStepModel struct {
	Name          types.String              `tfsdk:"name"`
	Agent         types.String              `tfsdk:"agent"`
	Prompt        types.String              `tfsdk:"prompt"`
	OutputSchema  types.String              `tfsdk:"output_schema"`
	MaxIterations types.Int64               `tfsdk:"max_iterations"`
	MaxRetries    types.Int64               `tfsdk:"max_retries"`
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
			"together by conditions over the step's structured output.\n\n" +
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
								"for the start step, the previous step's output for every other — is appended below it " +
								"under a header, so the prompt says what to do rather than restating the input.",
						},
						"output_schema": schema.StringAttribute{
							Optional: true,
							MarkdownDescription: "JSON Schema for the step's structured output, as a JSON string — " +
								"write it with `jsonencode`. A step that has it finishes by calling the builtin " +
								"terminal tool with conforming output. **Required when the step has any conditional " +
								"transition**, since there would otherwise be nothing for the condition to read.",
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
						"transition": schema.ListNestedBlock{
							MarkdownDescription: "A directed edge out of this step. **Order matters**: transitions are " +
								"tried in the order written and the first match wins, so put the catch-all last. " +
								"A step whose transitions all fail to match ends the run, carrying its output as the run's.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"to": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "Name of the step to go to.",
									},
									"condition": schema.StringAttribute{
										Optional: true,
										MarkdownDescription: "An expression over the producing step's structured output, " +
											"bound to `output`, evaluating to a boolean — e.g. `output.approved`. " +
											"Omit for an unconditional catch-all.",
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

// toWorkflowInput builds the request body, failing on a step whose
// output_schema is not JSON rather than letting horsie reject it as a 422 that
// does not say which step.
func toWorkflowInput(m workflowModel) (api.WorkflowInput, error) {
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
		if !s.OutputSchema.IsNull() && !s.OutputSchema.IsUnknown() {
			var decoded any
			if err := json.Unmarshal([]byte(s.OutputSchema.ValueString()), &decoded); err != nil {
				return in, fmt.Errorf("step %q: output_schema is not valid JSON: %w", step.Name, err)
			}
			step.OutputSchema = &decoded
		}
		if !s.MaxIterations.IsNull() {
			v := uint32(s.MaxIterations.ValueInt64())
			step.MaxIterations = &v
		}
		if !s.MaxRetries.IsNull() {
			v := uint32(s.MaxRetries.ValueInt64())
			step.MaxRetries = &v
		}
		// A terminal step writes no transition blocks at all, so absent and empty
		// mean the same thing here; send absent, which is what the view answers
		// with and therefore what keeps a second plan empty.
		if len(s.Transitions) > 0 {
			edges := make([]api.WorkflowTransition, 0, len(s.Transitions))
			for _, t := range s.Transitions {
				edge := api.WorkflowTransition{To: t.To.ValueString()}
				if !t.Condition.IsNull() {
					v := t.Condition.ValueString()
					edge.Condition = &v
				}
				edges = append(edges, edge)
			}
			step.Transitions = &edges
		}
		in.Steps = append(in.Steps, step)
	}
	return in, nil
}

// sameJSON reports whether two JSON documents mean the same thing. Used to keep
// the operator's own formatting of output_schema in state: horsie parses and
// re-serialises it, so a byte comparison would report drift on every plan over
// nothing but whitespace and key order.
func sameJSON(configured string, encoded []byte) bool {
	var a, b any
	if err := json.Unmarshal([]byte(configured), &a); err != nil {
		return false
	}
	if err := json.Unmarshal(encoded, &b); err != nil {
		return false
	}
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(x, y)
}

// outputSchemaString renders what the server holds, preferring the string the
// configuration used when the two are the same document.
func outputSchemaString(configured types.String, schema *any) types.String {
	if schema == nil {
		return types.StringNull()
	}
	encoded, err := json.Marshal(*schema)
	if err != nil {
		// Unreachable for anything that arrived as JSON, but silently dropping
		// the schema would look like the server forgot it.
		return types.StringValue(fmt.Sprintf("<unencodable: %v>", err))
	}
	if !configured.IsNull() && !configured.IsUnknown() && sameJSON(configured.ValueString(), encoded) {
		return configured
	}
	return types.StringValue(string(encoded))
}

func applyWorkflow(m *workflowModel, v *api.WorkflowView) {
	// The configured schema strings, by step name, so a refresh keeps the
	// operator's formatting where the document has not actually changed.
	configured := map[string]types.String{}
	for _, s := range m.Steps {
		configured[s.Name.ValueString()] = s.OutputSchema
	}

	m.Name = types.StringValue(v.Name)
	m.Description = types.StringValue(v.Description)
	m.Start = types.StringValue(v.Start)
	if v.MaxSteps != nil {
		m.MaxSteps = types.Int64Value(int64(*v.MaxSteps))
	} else {
		m.MaxSteps = types.Int64Null()
	}
	m.CreatedAt = types.StringValue(v.CreatedAt)
	m.UpdatedAt = types.StringValue(v.UpdatedAt)

	m.Steps = nil
	for _, s := range v.Steps {
		step := workflowStepModel{
			Name:          types.StringValue(s.Name),
			Agent:         types.StringValue(s.Agent),
			Prompt:        types.StringValue(s.Prompt),
			OutputSchema:  outputSchemaString(configured[s.Name], s.OutputSchema),
			MaxIterations: optInt64(s.MaxIterations),
			MaxRetries:    optInt64(s.MaxRetries),
		}
		if s.Transitions != nil {
			for _, t := range *s.Transitions {
				step.Transitions = append(step.Transitions, workflowTransitionModel{
					To:        types.StringValue(t.To),
					Condition: optString(t.Condition),
				})
			}
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
	in, err := toWorkflowInput(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid workflow", err.Error())
		return
	}
	view, err := r.client.CreateWorkflow(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Could not create workflow", err.Error())
		return
	}
	applyWorkflow(&plan, view)
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
	applyWorkflow(&state, view)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workflowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workflowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := toWorkflowInput(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid workflow", err.Error())
		return
	}
	view, err := r.client.ReplaceWorkflow(ctx, plan.Name.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Could not update workflow", err.Error())
		return
	}
	applyWorkflow(&plan, view)
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
