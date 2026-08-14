package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	api "github.com/blossomstack/terraform-provider-horsie/internal/horsieapi"
)

// horsie trims instructions on save, so a heredoc — which is how anyone writes
// a paragraph in HCL — comes back different from what was configured. Keeping
// the server's answer verbatim would report drift on every plan and never
// converge, because Terraform prefers the config value.
func TestInstructionsKeepTheConfiguredWhitespace(t *testing.T) {
	configured := "\n  Always cite file:line.\n"
	trimmed := "Always cite file:line."

	m := agentModel{
		Name: types.StringValue("reviewer"), Model: types.StringValue("sonnet"),
		Instructions: types.StringValue(configured),
	}
	r := &agentResource{}
	in := r.input(context.Background(), m)
	if in.Instructions == nil || *in.Instructions != trimmed {
		t.Fatalf("sent %v, want the trimmed instructions horsie would store", in.Instructions)
	}

	applyAgent(context.Background(), &m, &api.AgentView{
		Name: "reviewer", Model: "sonnet", Instructions: in.Instructions,
	})
	if m.Instructions.ValueString() != configured {
		t.Errorf("instructions = %q, want the configured string back unchanged", m.Instructions.ValueString())
	}
}

// An edit made in the WebUI must still show up, or Terraform would be blind to
// drift in the one field it cannot see any other way.
func TestInstructionsTakeTheServersWhenTheyDiffer(t *testing.T) {
	m := agentModel{
		Name: types.StringValue("reviewer"), Model: types.StringValue("sonnet"),
		Instructions: types.StringValue("Always cite file:line."),
	}
	served := "Never cite anything."
	applyAgent(context.Background(), &m, &api.AgentView{
		Name: "reviewer", Model: "sonnet", Instructions: &served,
	})
	if m.Instructions.ValueString() != served {
		t.Errorf("instructions = %q, want the server's", m.Instructions.ValueString())
	}
}

// Absent means "behave like an unpresetted agent", which is a different thing
// from empty instructions — the wire field has to stay off the body.
func TestOmittedInstructionsAndAutoCompactStayAbsent(t *testing.T) {
	r := &agentResource{}
	in := r.input(context.Background(), agentModel{
		Name: types.StringValue("a"), Model: types.StringValue("m"),
		Instructions: types.StringNull(), AutoCompact: types.BoolNull(),
	})
	if in.Instructions != nil {
		t.Errorf("instructions = %q, want absent", *in.Instructions)
	}
	if in.AutoCompact != nil {
		t.Errorf("autoCompact = %v, want absent so horsie applies its default", *in.AutoCompact)
	}

	m := agentModel{}
	applyAgent(context.Background(), &m, &api.AgentView{Name: "a", Model: "m"})
	if !m.Instructions.IsNull() || !m.AutoCompact.IsNull() {
		t.Errorf("instructions = %v, auto_compact = %v, want both null", m.Instructions, m.AutoCompact)
	}
}

// auto_compact is a tri-state on the wire: false is a real choice, not an
// omission, and sending it as absent would silently turn compaction back on.
func TestAutoCompactFalseIsSent(t *testing.T) {
	r := &agentResource{}
	in := r.input(context.Background(), agentModel{
		Name: types.StringValue("a"), Model: types.StringValue("m"),
		AutoCompact: types.BoolValue(false),
	})
	if in.AutoCompact == nil || *in.AutoCompact {
		t.Fatalf("autoCompact = %v, want false on the wire", in.AutoCompact)
	}
}
