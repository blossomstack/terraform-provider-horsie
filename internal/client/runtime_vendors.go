package client

import (
	"context"
	"net/http"
	"net/url"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Runtime vendors the server builds itself — a Fly app or a velos deployment.
//
// Only these are here. A vendor that dials in with `horsie connect` announces
// itself over a websocket and disappears when its link drops, so there is
// nothing to create or delete about one; those appear in the settings roster
// instead, which is what the runtime-vendors data source reads.

// ListRuntimeVendors returns every configured vendor, with credentials redacted
// to a boolean.
func (c *Client) ListRuntimeVendors(ctx context.Context) ([]api.RuntimeVendorConfigView, error) {
	var out []api.RuntimeVendorConfigView
	if err := c.Do(ctx, http.MethodGet, "/api/runtime-vendors", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRuntimeVendor returns one vendor, or a 404 Error if it is gone.
//
// The list endpoint is the only read horsie offers here, so this filters it
// rather than fetching a single row — the collection is a handful of entries.
func (c *Client) GetRuntimeVendor(ctx context.Context, name string) (*api.RuntimeVendorConfigView, error) {
	all, err := c.ListRuntimeVendors(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, &Error{Status: http.StatusNotFound, Method: http.MethodGet, Path: "/api/runtime-vendors/" + name}
}

// PutRuntimeVendor creates or fully replaces one vendor.
//
// Omitting Credential keeps the stored token, which is what lets a caller that
// can never read the secret round-trip the rest of the resource.
func (c *Client) PutRuntimeVendor(ctx context.Context, name string, in api.RuntimeVendorConfigInput) (*api.RuntimeVendorConfigView, error) {
	var out api.RuntimeVendorConfigView
	path := "/api/runtime-vendors/" + url.PathEscape(name)
	if err := c.Do(ctx, http.MethodPut, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRuntimeVendor forgets one vendor. Machines it created are left running:
// the server is no longer able to reach them, so destroying them would need the
// credential it is being told to forget.
func (c *Client) DeleteRuntimeVendor(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/api/runtime-vendors/"+url.PathEscape(name), nil, nil)
}
