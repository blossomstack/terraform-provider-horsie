package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Workflows: a named graph of steps, each an agent preset with a fixed prompt.

// ListWorkflows returns every workflow definition.
func (c *Client) ListWorkflows(ctx context.Context) ([]api.WorkflowView, error) {
	var out []api.WorkflowView
	if err := c.Do(ctx, http.MethodGet, "/api/workflows", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkflow returns one, or a 404 Error if it is gone.
func (c *Client) GetWorkflow(ctx context.Context, name string) (*api.WorkflowView, error) {
	var out api.WorkflowView
	if err := c.Do(ctx, http.MethodGet, "/api/workflows/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateWorkflow creates one. The name is in the body, not the path.
func (c *Client) CreateWorkflow(ctx context.Context, in api.WorkflowInput) (*api.WorkflowView, error) {
	var out api.WorkflowView
	if err := c.Do(ctx, http.MethodPost, "/api/workflows", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceWorkflow fully replaces one. Runs already under way keep their own
// snapshot of the graph, so this never rewrites history.
func (c *Client) ReplaceWorkflow(ctx context.Context, name string, in api.WorkflowInput) (*api.WorkflowView, error) {
	var out api.WorkflowView
	if err := c.Do(ctx, http.MethodPut, "/api/workflows/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWorkflow removes a definition. Its runs are ordinary sessions and stay.
func (c *Client) DeleteWorkflow(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/workflows/"+url.PathEscape(name), nil, nil)
}
