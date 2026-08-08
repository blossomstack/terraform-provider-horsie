package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Agent presets: a saved session configuration invoked with a message.

// ListAgents returns every agent preset.
func (c *Client) ListAgents(ctx context.Context) ([]api.AgentView, error) {
	var out []api.AgentView
	if err := c.Do(ctx, http.MethodGet, "/api/agents", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAgent returns one preset, or a 404 Error if it is gone.
func (c *Client) GetAgent(ctx context.Context, name string) (*api.AgentView, error) {
	var out api.AgentView
	if err := c.Do(ctx, http.MethodGet, "/api/agents/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAgent creates a preset. The name is in the body, not the path.
func (c *Client) CreateAgent(ctx context.Context, in api.AgentPresetInput) (*api.AgentView, error) {
	var out api.AgentView
	if err := c.Do(ctx, http.MethodPost, "/api/agents", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceAgent fully replaces a preset. Omitted list fields default to empty
// rather than being left alone, so callers send the whole preset.
func (c *Client) ReplaceAgent(ctx context.Context, name string, in api.AgentPresetInput) (*api.AgentView, error) {
	var out api.AgentView
	if err := c.Do(ctx, http.MethodPut, "/api/agents/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAgent removes a preset. The server refuses while a routine uses it.
func (c *Client) DeleteAgent(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/agents/"+url.PathEscape(name), nil, nil)
}

// Memory spaces: named namespaces the agent reads and writes at run time.
//
// Only the space is managed here. The memories inside it are written by the
// agent as it works, so putting them under Terraform would mean a plan that
// shows drift every time an agent thinks.

// ListMemorySpaces returns every memory space.
func (c *Client) ListMemorySpaces(ctx context.Context) ([]api.MemorySpaceView, error) {
	var out []api.MemorySpaceView
	if err := c.Do(ctx, http.MethodGet, "/api/memory-spaces", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMemorySpace filters the list; horsie exposes no per-space read.
func (c *Client) GetMemorySpace(ctx context.Context, name string) (*api.MemorySpaceView, error) {
	all, err := c.ListMemorySpaces(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, &Error{Status: http.StatusNotFound, Method: http.MethodGet, Path: "/api/memory-spaces/" + name}
}

// CreateMemorySpace creates a space.
func (c *Client) CreateMemorySpace(ctx context.Context, in api.MemorySpaceCreateInput) (*api.MemorySpaceView, error) {
	var out api.MemorySpaceView
	if err := c.Do(ctx, http.MethodPost, "/api/memory-spaces", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMemorySpace renames a space and/or changes its description. Omitted
// fields are left unchanged, and a rename carries the space's memories across.
func (c *Client) UpdateMemorySpace(ctx context.Context, name string, in api.MemorySpaceUpdateInput) (*api.MemorySpaceView, error) {
	var out api.MemorySpaceView
	if err := c.Do(ctx, http.MethodPut, "/api/memory-spaces/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteMemorySpace removes a space and everything in it.
func (c *Client) DeleteMemorySpace(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/memory-spaces/"+url.PathEscape(name), nil, nil)
}
