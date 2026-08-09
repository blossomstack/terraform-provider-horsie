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

// GetSettings returns the whole redacted settings view: models, model
// providers, the live runtime-vendor roster, and the default runtime vendor.
//
// Everything here has a narrower endpoint except the roster and the default,
// which are read-only projections of state horsie assembles from several
// places, so this is their only read.
func (c *Client) GetSettings(ctx context.Context) (*api.SettingsView, error) {
	var out api.SettingsView
	if err := c.Do(ctx, http.MethodGet, "/api/config", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetDefaultRuntimeVendor sets the vendor new sessions target when they name
// none, and returns the settings view horsie answers with.
func (c *Client) SetDefaultRuntimeVendor(ctx context.Context, vendor string) (*api.SettingsView, error) {
	var out api.SettingsView
	in := api.DefaultRuntimeVendorInput{Vendor: vendor}
	if err := c.Do(ctx, http.MethodPut, "/api/config/default-runtime-vendor", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ClearDefaultRuntimeVendor forgets the preference, falling back to `local`.
//
// A DELETE rather than a PUT of "": horsie refuses an empty vendor name, so
// this is the only way to say "no preference" rather than "a broken one".
func (c *Client) ClearDefaultRuntimeVendor(ctx context.Context) (*api.SettingsView, error) {
	var out api.SettingsView
	if err := c.Do(ctx, http.MethodDelete, "/api/config/default-runtime-vendor", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

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
