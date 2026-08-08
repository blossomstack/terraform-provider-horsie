package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// The model-configuration endpoints.
//
// They are named `model-providers` rather than `providers` because "vendor"
// already means the execution runtime in horsie, so a bare "provider" beside it
// reads as if it configured runtimes.

// ListModelProviders returns every configured LLM provider, with secrets
// redacted to a boolean.
func (c *Client) ListModelProviders(ctx context.Context) ([]api.ProviderView, error) {
	var out []api.ProviderView
	if err := c.Do(ctx, http.MethodGet, "/api/config/model-providers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModelProvider returns one provider, or a 404 Error if it is gone.
//
// The list endpoint is the only read horsie offers here, so this filters it
// rather than fetching a single row — the collection is a handful of entries.
func (c *Client) GetModelProvider(ctx context.Context, name string) (*api.ProviderView, error) {
	all, err := c.ListModelProviders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, &Error{Status: http.StatusNotFound, Method: http.MethodGet, Path: "/api/config/model-providers/" + name}
}

// PutModelProvider creates or replaces one provider.
//
// Omitting APIKey keeps the stored key and "" clears it, which is what lets a
// caller that never sees the secret round-trip the rest of the resource.
func (c *Client) PutModelProvider(ctx context.Context, name string, in api.ProviderInput) (*api.ProviderView, error) {
	var out api.ProviderView
	path := "/api/config/model-providers/" + url.PathEscape(name)
	if err := c.Do(ctx, http.MethodPut, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteModelProvider removes one provider. The server answers 409 while any
// model still routes to it.
func (c *Client) DeleteModelProvider(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/config/model-providers/"+url.PathEscape(name), nil, nil)
}

// ListModels returns every configured model alias.
func (c *Client) ListModels(ctx context.Context) ([]api.ModelView, error) {
	var out []api.ModelView
	if err := c.Do(ctx, http.MethodGet, "/api/config/models", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetModel returns one model alias, or a 404 Error if it is gone.
func (c *Client) GetModel(ctx context.Context, alias string) (*api.ModelView, error) {
	all, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Alias == alias {
			return &all[i], nil
		}
	}
	return nil, &Error{Status: http.StatusNotFound, Method: http.MethodGet, Path: "/api/config/models/" + alias}
}

// PutModel creates or replaces one model alias.
func (c *Client) PutModel(ctx context.Context, alias string, in api.ModelInput) (*api.ModelView, error) {
	var out api.ModelView
	path := "/api/config/models/" + url.PathEscape(alias)
	if err := c.Do(ctx, http.MethodPut, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteModel removes one model alias.
func (c *Client) DeleteModel(ctx context.Context, alias string) error {
	return c.Do(ctx, http.MethodDelete, "/api/config/models/"+url.PathEscape(alias), nil, nil)
}
