package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// Every field is non-zero on purpose. A field horsie adds later arrives here as
// a zero value, which this catches — nothing else does, since the unit tests in
// this package drive a fake built from the same generated types and so agree
// with any mistake they contain.
func flyModel() []flySettingsModel {
	return []flySettingsModel{{
		App:           types.StringValue("horsie-runtimes"),
		Image:         types.StringValue("ghcr.io/x/runtime:1"),
		Region:        types.StringValue("iad"),
		WorkspaceRoot: types.StringValue("/workspaces"),
		CallbackURL:   types.StringValue("wss://horsie.example.com/api/runtime/connect"),
		Volumes:       types.BoolValue(true),
		CPUKind:       types.StringValue("performance"),
		CPUs:          types.Int64Value(2),
		MemoryMB:      types.Int64Value(2048),
		VolumeSizeGB:  types.Int64Value(20),
	}}
}

func velosModel() []velosSettingsModel {
	return []velosSettingsModel{{
		ServerURL:     types.StringValue("http://velos:8080"),
		Image:         types.StringValue("ghcr.io/x/runtime:1"),
		RuntimeBin:    types.StringValue("/usr/bin/horsie-runtime"),
		WorkspaceRoot: types.StringValue("/workspaces"),
		CallbackURL:   types.StringValue("ws://horsie.internal:3789/api/runtime/connect"),
		CPU:           types.Int64Value(4),
		MemoryMB:      types.Int64Value(4096),
	}}
}

func TestFlySettingsRoundTrip(t *testing.T) {
	m := runtimeVendorModel{Name: types.StringValue("fly-prod"), Fly: flyModel()}
	got, err := m.settings()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := got.Variant.(api.RuntimeVendorSettingsFly)
	if !ok {
		t.Fatalf("variant = %T, want Fly", got.Variant)
	}
	want := api.FlyVendorSettings{
		App: "horsie-runtimes", Image: "ghcr.io/x/runtime:1", Region: "iad",
		WorkspaceRoot: "/workspaces",
		CallbackURL:   "wss://horsie.example.com/api/runtime/connect",
		Volumes:       true, CPUKind: "performance",
		Cpus: 2, MemoryMb: 2048, VolumeSizeGb: 20,
	}
	if v.Value != want {
		t.Errorf("fly settings = %#v\nwant %#v", v.Value, want)
	}

	var back runtimeVendorModel
	back.applyView(api.RuntimeVendorConfigView{Name: "fly-prod", Settings: got, HasCredential: true})
	if len(back.Fly) != 1 || len(back.Velos) != 0 {
		t.Fatalf("applyView chose the wrong block: fly=%v velos=%v", back.Fly, back.Velos)
	}
	if back.Fly[0] != flyModel()[0] {
		t.Errorf("round trip lost a field: %#v", back.Fly)
	}
	if back.Name.ValueString() != "fly-prod" || !back.HasCredential.ValueBool() {
		t.Errorf("name or has_credential was dropped: %#v", back)
	}
}

func TestVelosSettingsRoundTrip(t *testing.T) {
	m := runtimeVendorModel{Name: types.StringValue("velos-lab"), Velos: velosModel()}
	got, err := m.settings()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := got.Variant.(api.RuntimeVendorSettingsVelos)
	if !ok {
		t.Fatalf("variant = %T, want Velos", got.Variant)
	}
	want := api.VelosVendorSettings{
		ServerURL: "http://velos:8080", Image: "ghcr.io/x/runtime:1",
		RuntimeBin: "/usr/bin/horsie-runtime", WorkspaceRoot: "/workspaces",
		CallbackURL: "ws://horsie.internal:3789/api/runtime/connect",
		CPU:         4, MemoryMb: 4096,
	}
	if v.Value != want {
		t.Errorf("velos settings = %#v\nwant %#v", v.Value, want)
	}

	var back runtimeVendorModel
	back.applyView(api.RuntimeVendorConfigView{Name: "velos-lab", Settings: got})
	if len(back.Velos) != 1 || len(back.Fly) != 0 {
		t.Fatalf("applyView chose the wrong block: fly=%v velos=%v", back.Fly, back.Velos)
	}
	if back.Velos[0] != velosModel()[0] {
		t.Errorf("round trip lost a field: %#v", back.Velos)
	}
}

// The block present is the kind. With none the generated marshaller fails with
// "has no variant set" partway through an apply, and with both the
// configuration is describing two substrates at once — so both are refused
// here, and again at validate time where Terraform can point at the block.
func TestSettingsRefusesZeroOrTwoBlocks(t *testing.T) {
	if _, err := (runtimeVendorModel{}).settings(); err == nil {
		t.Error("no block: want an error")
	}
	both := runtimeVendorModel{Fly: flyModel(), Velos: velosModel()}
	if _, err := both.settings(); err == nil {
		t.Error("two blocks: want an error")
	}
}

// applyView replaces whatever was there rather than merging into it, so a
// vendor whose kind changed underneath Terraform reads back as the kind it
// actually is.
func TestApplyViewClearsTheOtherBlock(t *testing.T) {
	m := runtimeVendorModel{Fly: flyModel()}
	settings, err := (runtimeVendorModel{Velos: velosModel()}).settings()
	if err != nil {
		t.Fatal(err)
	}
	m.applyView(api.RuntimeVendorConfigView{Name: "x", Settings: settings})
	if len(m.Fly) != 0 {
		t.Error("the stale fly block survived")
	}
	if len(m.Velos) != 1 {
		t.Error("the velos block was not set")
	}
}
