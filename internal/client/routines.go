package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Routines: an agent preset plus a fixed prompt and a trigger.

// ListRoutines returns every routine.
func (c *Client) ListRoutines(ctx context.Context) ([]api.RoutineView, error) {
	var out []api.RoutineView
	if err := c.Do(ctx, http.MethodGet, "/api/routines", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRoutine returns one routine, or a 404 Error if it is gone.
func (c *Client) GetRoutine(ctx context.Context, name string) (*api.RoutineView, error) {
	var out api.RoutineView
	if err := c.Do(ctx, http.MethodGet, "/api/routines/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRoutine creates one. The name is in the body, not the path.
func (c *Client) CreateRoutine(ctx context.Context, in api.RoutineInput) (*api.RoutineView, error) {
	var out api.RoutineView
	if err := c.Do(ctx, http.MethodPost, "/api/routines", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceRoutine fully replaces one.
func (c *Client) ReplaceRoutine(ctx context.Context, name string, in api.RoutineInput) (*api.RoutineView, error) {
	var out api.RoutineView
	if err := c.Do(ctx, http.MethodPut, "/api/routines/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRoutine removes one.
func (c *Client) DeleteRoutine(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/routines/"+url.PathEscape(name), nil, nil)
}
