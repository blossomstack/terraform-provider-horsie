package provider

import (
	"context"
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

func outcomes(values ...string) types.List {
	l, _ := types.ListValueFrom(context.Background(), types.StringType, values)
	return l
}

func catchAll(to string) workflowTransitionModel {
	return workflowTransitionModel{
		To:               types.StringValue(to),
		WhenOutcomeIn:    types.ListNull(types.StringType),
		WhenOutcomeNotIn: types.ListNull(types.StringType),
	}
}

// The graph must survive HCL → wire → HCL unchanged, including transition
// order: transitions are tried in order and the first match wins, so a reorder
// is a behaviour change, not cosmetic.
func TestWorkflowRoundTripsTheGraph(t *testing.T) {
	ctx := context.Background()
	gated := catchAll("fix")
	gated.WhenOutcomeNotIn = outcomes("approved")

	m := workflowModel{
		Name:        types.StringValue("review-and-fix"),
		Description: types.StringValue("loop until approved"),
		Start:       types.StringValue("review"),
		MaxSteps:    types.Int64Value(12),
		Steps: []workflowStepModel{
			step("review", gated, catchAll("summarise")),
			step("fix", catchAll("review")),
			step("summarise"),
		},
	}
	m.Steps[0].Outcomes = []workflowOutcomeModel{
		{Value: types.StringValue("approved"), Description: types.StringValue("ready to merge")},
		{Value: types.StringValue("changes"), Description: types.StringValue("needs another pass")},
	}
	m.Steps[0].Fields = []workflowFieldModel{{
		Name:        types.StringValue("blockers"),
		Kind:        types.StringValue(string(api.StepFieldTypeStringList)),
		Description: types.StringValue("what still has to change"),
		Required:    types.BoolValue(true),
	}}
	m.Steps[1].MaxIterations = types.Int64Value(40)
	m.Steps[2].Interactive = types.BoolValue(true)

	in, err := toWorkflowInput(ctx, m)
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
	applyWorkflow(ctx, &got, &view)

	if len(got.Steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(got.Steps))
	}
	if got.Steps[0].Transitions[0].To.ValueString() != "fix" ||
		got.Steps[0].Transitions[1].To.ValueString() != "summarise" {
		t.Error("transition order was not preserved; first match wins, so order is behaviour")
	}
	if !got.Steps[0].Transitions[1].WhenOutcomeIn.IsNull() ||
		!got.Steps[0].Transitions[1].WhenOutcomeNotIn.IsNull() {
		t.Error("an unconditional catch-all must round-trip with both filters null")
	}
	if len(got.Steps[0].Outcomes) != 2 || got.Steps[0].Outcomes[1].Value.ValueString() != "changes" {
		t.Errorf("outcomes = %v, want both in order", got.Steps[0].Outcomes)
	}
	if len(got.Steps[0].Fields) != 1 ||
		got.Steps[0].Fields[0].Kind.ValueString() != "StringList" ||
		!got.Steps[0].Fields[0].Required.ValueBool() {
		t.Errorf("result_field = %v, want the declared field back", got.Steps[0].Fields)
	}
	if got.Steps[1].MaxIterations.ValueInt64() != 40 {
		t.Error("max_iterations was lost")
	}
	if !got.Steps[2].Interactive.ValueBool() {
		t.Error("interactive was lost")
	}
	if got.Steps[2].Transitions != nil {
		t.Error("a terminal step must round-trip with no transitions, not an empty list")
	}
	if got.MaxSteps.ValueInt64() != 12 {
		t.Error("max_steps was lost")
	}
}

// The two flat arguments are the two operators of one wire union, so each must
// land on its own variant and come back on the same one. Getting this wrong is
// silent: horsie reads an unknown tag as a catch-all rather than refusing it.
func TestOutcomeFilterMapsToItsOperator(t *testing.T) {
	ctx := context.Background()
	in := catchAll("b")
	in.WhenOutcomeIn = outcomes("p0", "p1")
	notIn := catchAll("c")
	notIn.WhenOutcomeNotIn = outcomes("wontfix")

	built, err := toWorkflowInput(ctx, workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{step("a", in, notIn, catchAll("d"))},
	})
	if err != nil {
		t.Fatalf("toWorkflowInput: %v", err)
	}
	edges := *built.Steps[0].Transitions
	if v, ok := edges[0].When.Variant.(api.OutcomeFilterIn); !ok {
		t.Errorf("when_outcome_in built %T, want OutcomeFilterIn", edges[0].When.Variant)
	} else if len(v.Value.Values) != 2 || v.Value.Values[0] != "p0" {
		t.Errorf("values = %v, want [p0 p1] in order", v.Value.Values)
	}
	if v, ok := edges[1].When.Variant.(api.OutcomeFilterNotIn); !ok {
		t.Errorf("when_outcome_not_in built %T, want OutcomeFilterNotIn", edges[1].When.Variant)
	} else if len(v.Value.Values) != 1 || v.Value.Values[0] != "wontfix" {
		t.Errorf("values = %v, want [wontfix]", v.Value.Values)
	}
	if edges[2].When != nil {
		t.Errorf("when = %v, want absent for the catch-all", edges[2].When)
	}

	got := workflowModel{}
	applyWorkflow(ctx, &got, &api.WorkflowView{Name: "w", Start: "a", Steps: built.Steps})
	back := got.Steps[0].Transitions
	if back[0].WhenOutcomeIn.IsNull() || !back[0].WhenOutcomeNotIn.IsNull() {
		t.Error("an `in` filter came back on the wrong argument")
	}
	if !back[1].WhenOutcomeIn.IsNull() || back[1].WhenOutcomeNotIn.IsNull() {
		t.Error("a `not_in` filter came back on the wrong argument")
	}
}

// A transition setting both operators cannot be marshalled — the union holds
// one variant. ValidateConfig catches it at plan time; this is the backstop
// that keeps it from reaching the wire as a silently dropped filter.
func TestBothOutcomeFiltersIsRefused(t *testing.T) {
	both := catchAll("b")
	both.WhenOutcomeIn = outcomes("p0")
	both.WhenOutcomeNotIn = outcomes("p1")

	_, err := toWorkflowInput(context.Background(), workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{step("a", both)},
	})
	if err == nil || !strings.Contains(err.Error(), "a") {
		t.Fatalf("err = %v, want it to name the step", err)
	}
}

// A step that declares no outcomes reports success/failure, and the view
// answers with them absent. Sending an empty list instead earns a 422.
func TestNoOutcomesStaysAbsent(t *testing.T) {
	ctx := context.Background()
	in, err := toWorkflowInput(ctx, workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{step("a")},
	})
	if err != nil {
		t.Fatalf("toWorkflowInput: %v", err)
	}
	if in.Steps[0].Outcomes != nil {
		t.Errorf("outcomes = %v, want absent", *in.Steps[0].Outcomes)
	}
	if in.Steps[0].Fields != nil {
		t.Errorf("fields = %v, want absent", *in.Steps[0].Fields)
	}
	got := workflowModel{}
	applyWorkflow(ctx, &got, &api.WorkflowView{Name: "w", Start: "a", Steps: in.Steps})
	if got.Steps[0].Outcomes != nil {
		t.Errorf("outcomes came back as %v, want none", got.Steps[0].Outcomes)
	}
}

// An omitted max_steps means "the server's default" and the view answers with
// it absent, so it must stay null rather than becoming 0.
func TestOmittedMaxStepsStaysNull(t *testing.T) {
	ctx := context.Background()
	m := workflowModel{
		Name: types.StringValue("w"), Start: types.StringValue("a"),
		Steps: []workflowStepModel{step("a")},
	}
	in, err := toWorkflowInput(ctx, m)
	if err != nil {
		t.Fatalf("toWorkflowInput: %v", err)
	}
	if in.MaxSteps != nil {
		t.Fatalf("maxSteps = %v, want absent", *in.MaxSteps)
	}
	got := m
	applyWorkflow(ctx, &got, &api.WorkflowView{Name: "w", Start: "a", Steps: in.Steps})
	if !got.MaxSteps.IsNull() {
		t.Errorf("max_steps = %v, want null", got.MaxSteps)
	}
}
