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

	schema := any(map[string]any{"type": "object"})
	max := uint32(12)
	view, err := c.CreateWorkflow(context.Background(), api.WorkflowInput{
		Name: "review", Start: "a", MaxSteps: &max,
		Steps: []api.WorkflowStepDef{{
			Name: "a", Agent: "r", Prompt: "p", OutputSchema: &schema,
			Transitions: &[]api.WorkflowTransition{{To: "b"}},
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
	if _, ok := first["outputSchema"]; !ok {
		t.Errorf("outputSchema missing or not camelCased: %v", first)
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
