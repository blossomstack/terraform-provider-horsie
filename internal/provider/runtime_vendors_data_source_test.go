package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// runtimeVendorObjectType is written by hand because the model holds a
// types.List: every attribute of a data source is Computed, and a Go slice
// cannot hold the unknown one carries before refresh. Hand-written and schema
// drift silently, so pin them together — an attribute added to the nested
// object without being added to the element type fails here rather than at
// refresh, where the error names neither.
func TestRuntimeVendorObjectTypeMatchesTheSchema(t *testing.T) {
	var resp datasource.SchemaResponse
	NewRuntimeVendorsDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	attr, ok := resp.Schema.Attributes["vendors"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("vendors = %T, want a ListNestedAttribute", resp.Schema.Attributes["vendors"])
	}
	if want := attr.NestedObject.Type(); !runtimeVendorObjectType.Equal(want) {
		t.Errorf("element type drifted:\n got %s\nwant %s", runtimeVendorObjectType, want)
	}
}

// Everything a data source returns is Computed, and the whole point of the
// roster is that it includes vendors Terraform did not create.
func TestRuntimeVendorsSchemaIsAllComputed(t *testing.T) {
	var resp datasource.SchemaResponse
	NewRuntimeVendorsDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &resp)

	attr := resp.Schema.Attributes["vendors"].(schema.ListNestedAttribute)
	if !attr.IsComputed() || attr.IsRequired() || attr.IsOptional() {
		t.Error("vendors must be Computed and nothing else")
	}
	for name, nested := range attr.NestedObject.Attributes {
		if !nested.IsComputed() {
			t.Errorf("%s is not Computed", name)
		}
	}
	if _, ok := attr.NestedObject.Attributes["supports_provisioning"].(schema.BoolAttribute); !ok {
		t.Error("supports_provisioning must be a bool")
	}
	if _, ok := attr.NestedObject.Attributes["name"].(schema.StringAttribute); !ok {
		t.Error("name must be a string")
	}
	if _, ok := runtimeVendorObjectType.AttrTypes["is_default"]; !ok {
		t.Error("is_default is missing from the element type")
	}
	if runtimeVendorObjectType.AttrTypes["name"] != types.StringType {
		t.Error("name must be a string in the element type too")
	}
}
