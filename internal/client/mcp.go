package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Remote MCP servers: an endpoint plus how horsie authenticates to it.

// ListMcpServers returns every configured server.
func (c *Client) ListMcpServers(ctx context.Context) ([]api.McpServerView, error) {
	var out api.McpServerList
	if err := c.Do(ctx, http.MethodGet, "/api/mcp/servers", nil, &out); err != nil {
		return nil, err
	}
	return out.Servers, nil
}

// GetMcpServer returns one server by name, or a 404 Error if it is gone.
//
// horsie exposes the list and no per-server GET, so this filters and
// synthesises the 404 that Read relies on to tell "removed outside Terraform"
// from "the call failed".
func (c *Client) GetMcpServer(ctx context.Context, name string) (*api.McpServerView, error) {
	all, err := c.ListMcpServers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, &Error{
		Status: http.StatusNotFound,
		Method: http.MethodGet,
		Path:   "/api/mcp/servers",
		Body:   "no MCP server named " + name,
	}
}

// PutMcpServer upserts one. The path is the id of record and overrides any
// name in the body.
func (c *Client) PutMcpServer(ctx context.Context, name string, in api.McpServerInput) (*api.McpServerView, error) {
	var out api.McpServerView
	if err := c.Do(ctx, http.MethodPut, "/api/mcp/servers/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteMcpServer removes one.
func (c *Client) DeleteMcpServer(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/mcp/servers/"+url.PathEscape(name), nil, nil)
}
