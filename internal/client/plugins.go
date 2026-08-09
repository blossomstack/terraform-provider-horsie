package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Plugin bundles: skills, commands, agents and hooks, installed from a git repo.

// ListPlugins returns the installed bundle library.
func (c *Client) ListPlugins(ctx context.Context) ([]api.PluginView, error) {
	var out []api.PluginView
	if err := c.Do(ctx, http.MethodGet, "/api/plugins", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlugin returns one bundle by name, or a 404 Error if it is gone.
//
// horsie has no per-bundle GET — the library is small and the settings page
// wants it whole — so this filters the list and synthesises the 404 every Read
// relies on to tell "removed outside Terraform" from "the call failed".
func (c *Client) GetPlugin(ctx context.Context, name string) (*api.PluginView, error) {
	all, err := c.ListPlugins(ctx)
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
		Path:   "/api/plugins",
		Body:   "no bundle named " + name,
	}
}

// InstallPlugin installs a bundle, or registers the catalogue a URL turned out
// to be — the caller has to handle both outcomes.
func (c *Client) InstallPlugin(ctx context.Context, in api.PluginInstallInput) (*api.InstallOutcome, error) {
	var out api.InstallOutcome
	if err := c.Do(ctx, http.MethodPost, "/api/plugins", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetPluginDefault toggles whether a bundle is pre-selected for new sessions.
func (c *Client) SetPluginDefault(ctx context.Context, name string, enabled bool) (*api.PluginView, error) {
	var out api.PluginView
	in := api.PluginDefaultInput{EnabledDefault: enabled}
	if err := c.Do(ctx, http.MethodPut, "/api/plugins/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePlugin removes a bundle and garbage-collects its artifact.
func (c *Client) DeletePlugin(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/plugins/"+url.PathEscape(name), nil, nil)
}
