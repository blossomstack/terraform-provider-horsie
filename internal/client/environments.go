package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Environments: a reusable runtime + repos bundle.
//
// An environment names its runtime vendor — the opposite pole from an agent
// preset, which deliberately names none.

// ListEnvironments returns every environment.
func (c *Client) ListEnvironments(ctx context.Context) ([]api.EnvironmentView, error) {
	var out []api.EnvironmentView
	if err := c.Do(ctx, http.MethodGet, "/api/environments", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetEnvironment returns one environment, or a 404 Error if it is gone.
func (c *Client) GetEnvironment(ctx context.Context, name string) (*api.EnvironmentView, error) {
	var out api.EnvironmentView
	if err := c.Do(ctx, http.MethodGet, "/api/environments/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEnvironment creates one. The name is in the body, not the path.
func (c *Client) CreateEnvironment(ctx context.Context, in api.EnvironmentInput) (*api.EnvironmentView, error) {
	var out api.EnvironmentView
	if err := c.Do(ctx, http.MethodPost, "/api/environments", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceEnvironment fully replaces one; omitted list fields default to empty.
func (c *Client) ReplaceEnvironment(ctx context.Context, name string, in api.EnvironmentInput) (*api.EnvironmentView, error) {
	var out api.EnvironmentView
	if err := c.Do(ctx, http.MethodPut, "/api/environments/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEnvironment removes one.
func (c *Client) DeleteEnvironment(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/environments/"+url.PathEscape(name), nil, nil)
}
