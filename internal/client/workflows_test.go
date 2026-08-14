package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

func TestCreateWorkflowPostsToTheCollection(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(api.WorkflowView{
			Name: "review", Start: "a", CreatedAt: "1", UpdatedAt: "1",
			Steps: []api.WorkflowStepDef{{Name: "a", Agent: "r", Prompt: "p"}},
		})
	})

	max, iterations := uint32(12), uint32(40)
	view, err := c.CreateWorkflow(context.Background(), api.WorkflowInput{
		Name: "review", Start: "a", MaxSteps: &max,
		Steps: []api.WorkflowStepDef{{
			Name: "a", Agent: "r", Prompt: "p", MaxIterations: &iterations,
			Outcomes: &[]api.StepOutcome{{Value: "p0", Description: "ship it"}},
			Transitions: &[]api.WorkflowTransition{{To: "b", When: &api.OutcomeFilter{
				Variant: api.OutcomeFilterIn{Value: api.OutcomeIn{Values: []string{"p0"}}},
			}}},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/workflows" {
		t.Errorf("%s %s, want POST /api/workflows", gotMethod, gotPath)
	}
	if _, ok := gotBody["maxSteps"]; !ok {
		t.Errorf("maxSteps was not camelCased on the wire: %v", gotBody)
	}
	steps, ok := gotBody["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %v, want one", gotBody["steps"])
	}
	first, _ := steps[0].(map[string]any)
	if _, ok := first["maxIterations"]; !ok {
		t.Errorf("maxIterations missing or not camelCased: %v", first)
	}
	// The filter is an adjacently tagged union: horsie reads the operator from
	// `op`, and a body that spelled it any other way is accepted as a
	// catch-all rather than refused.
	edges, ok := first["transitions"].([]any)
	if !ok || len(edges) != 1 {
		t.Fatalf("transitions = %v, want one", first["transitions"])
	}
	when, ok := edges[0].(map[string]any)["when"].(map[string]any)
	if !ok {
		t.Fatalf("when = %v, want the union object", edges[0])
	}
	if when["op"] != "In" {
		t.Errorf("op = %v, want In", when["op"])
	}
	if view.Name != "review" {
		t.Errorf("name = %q, want review", view.Name)
	}
}

func TestReplaceWorkflowPutsByName(t *testing.T) {
	var gotPath, gotMethod string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewEncoder(w).Encode(api.WorkflowView{Name: "a b", Start: "s", CreatedAt: "1", UpdatedAt: "1"})
	})
	if _, err := c.ReplaceWorkflow(context.Background(), "a b", api.WorkflowInput{Name: "a b", Start: "s"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/workflows/a b" {
		t.Errorf("%s %s, want PUT /api/workflows/a b", gotMethod, gotPath)
	}
}

func TestGetWorkflowMissingIsNotFound(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.GetWorkflow(context.Background(), "gone"); !IsNotFound(err) {
		t.Fatalf("err = %v, want a 404 so Read drops the resource from state", err)
	}
}
