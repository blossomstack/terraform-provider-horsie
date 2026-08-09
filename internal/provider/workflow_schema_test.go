package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func step(name string, transitions ...workflowTransitionModel) workflowStepModel {
	return workflowStepModel{
		Name:        types.StringValue(name),
		Agent:       types.StringValue("reviewer"),
		Prompt:      types.StringValue("do the thing"),
		Transitions: transitions,
	}
}

// The graph must survive HCL → wire → HCL unchanged, including transition
// order: transitions are tried in order and the first match wins, so a reorder
// is a behaviour change, not cosmetic.
func TestWorkflowRoundTripsTheGraph(t *testing.T) {
	m := workflowModel{
		Name:        types.StringValue("review-and-fix"),
		Description: types.StringValue("loop until approved"),
		Start:       types.StringValue("review"),
		MaxSteps:    types.Int64Value(12),
		Steps: []workflowStepModel{
			step("review",
				workflowTransitionModel{To: types.StringValue("fix"), Condition: types.StringValue("!output.approved")},
				workflowTransitionModel{To: types.StringValue("summarise")},
			),
			step("fix", workflowTransitionModel{To: types.StringValue("review")}),
			step("summarise"),
		},
	}
	m.Steps[1].MaxIterations = types.Int64Value(40)

	in, err := toWorkflowInput(m)
	if err != nil {
		t.Fatalf("toWorkflowInput: %v", err)
	}

	// The view echoes the definition, so build one from the input.
	view := api.WorkflowView{
		Name: in.Name, Description: *in.Description, Start: in.Start,
		Steps: in.Steps, MaxSteps: in.MaxSteps,
		CreatedAt: "1", UpdatedAt: "1",
	}
	got := m
	applyWorkflow(&got, &view)

	if len(got.Steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(got.Steps))
	}
	if got.Steps[0].Transitions[0].To.ValueString() != "fix" ||
		got.Steps[0].Transitions[1].To.ValueString() != "summarise" {
		t.Error("transition order was not preserved; first match wins, so order is behaviour")
	}
	if !got.Steps[0].Transitions[1].Condition.IsNull() {
		t.Error("an unconditional catch-all must round-trip as a null condition")
	}
	if got.Steps[1].MaxIterations.ValueInt64() != 40 {
		t.Error("max_iterations was lost")
	}
	if got.Steps[2].Transitions != nil {
		t.Error("a terminal step must round-trip with no transitions, not an empty list")
	}
	if got.MaxSteps.ValueInt64() != 12 {
		t.Error("max_steps was lost")
	}
}

// An omitted max_steps means "the server's default" and the view answers with
// it absent, so it must stay null rather than becoming 0.
func TestOmittedMaxStepsStaysNull(t *testing.T) {
	m := workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{step("a")},
	}
	in, err := toWorkflowInput(m)
	if err != nil {
		t.Fatalf("toWorkflowInput: %v", err)
	}
	if in.MaxSteps != nil {
		t.Fatalf("maxSteps = %v, want absent", *in.MaxSteps)
	}
	got := m
	applyWorkflow(&got, &api.WorkflowView{Name: "w", Start: "a", Steps: in.Steps})
	if !got.MaxSteps.IsNull() {
		t.Errorf("max_steps = %v, want null", got.MaxSteps)
	}
}

// A schema that is not JSON should name the step it came from; horsie's 422
// does not.
func TestBadOutputSchemaNamesTheStep(t *testing.T) {
	m := workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{{
			Name: types.StringValue("triage"), Agent: types.StringValue("x"),
			Prompt: types.StringValue("p"), OutputSchema: types.StringValue("{not json"),
		}},
	}
	_, err := toWorkflowInput(m)
	if err == nil || !strings.Contains(err.Error(), "triage") {
		t.Fatalf("err = %v, want it to name the step", err)
	}
}

// horsie parses and re-serialises the schema, so comparing bytes would report
// drift over nothing but whitespace and key order.
func TestOutputSchemaKeepsTheConfiguredFormatting(t *testing.T) {
	configured := "{\n  \"type\": \"object\",\n  \"required\": [\"ok\"]\n}"
	var parsed any
	if err := json.Unmarshal([]byte(configured), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := outputSchemaString(types.StringValue(configured), &parsed)
	if got.ValueString() != configured {
		t.Errorf("got %q, want the configured string back unchanged", got.ValueString())
	}
}

// A schema that genuinely changed on the server must win, or drift introduced
// outside Terraform would be invisible.
func TestOutputSchemaTakesTheServersWhenItDiffers(t *testing.T) {
	var parsed any
	if err := json.Unmarshal([]byte(`{"type":"string"}`), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := outputSchemaString(types.StringValue(`{"type":"object"}`), &parsed)
	if !strings.Contains(got.ValueString(), "string") {
		t.Errorf("got %q, want the server's schema", got.ValueString())
	}
}

func TestOutputSchemaAbsentIsNull(t *testing.T) {
	if got := outputSchemaString(types.StringNull(), nil); !got.IsNull() {
		t.Errorf("got %v, want null", got)
	}
}
