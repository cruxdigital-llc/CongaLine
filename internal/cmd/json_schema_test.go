package cmd

import (
	"strings"
	"testing"
)

// TestCommandSchema_AddUser_RoleField guards the contract between the
// CLI --role flag and downstream operators / SDK generators that consume
// `conga json-schema admin.add-user`. PR #53 added the role package
// system; if the schema entry is removed or renamed, automated callers
// lose the documented hook for choosing a role at provision time.
func TestCommandSchema_AddUser_RoleField(t *testing.T) {
	schema, ok := commandSchemas["admin.add-user"]
	if !ok {
		t.Fatal("commandSchemas missing admin.add-user")
	}
	if schema.Input == nil {
		t.Fatal("admin.add-user schema has no Input section")
	}

	role, ok := schema.Input.Fields["role"]
	if !ok {
		t.Fatal("admin.add-user input missing 'role' field — role-package flag is part of the CLI surface")
	}
	if role.Type != "string" {
		t.Errorf("role.type = %q, want \"string\"", role.Type)
	}
	if !strings.Contains(role.Description, "role-") {
		t.Errorf("role description should reference role-* slugs, got %q", role.Description)
	}
	if !strings.Contains(role.Description, "type: user") {
		t.Errorf("role description must document the type: user constraint, got %q", role.Description)
	}
}

// TestCommandSchema_AddTeam_RoleField is the sibling guard for the
// team-side schema. The role.meta type-match is documented at this
// level so callers don't have to read source to discover the
// constraint.
func TestCommandSchema_AddTeam_RoleField(t *testing.T) {
	schema, ok := commandSchemas["admin.add-team"]
	if !ok {
		t.Fatal("commandSchemas missing admin.add-team")
	}
	if schema.Input == nil {
		t.Fatal("admin.add-team schema has no Input section")
	}

	role, ok := schema.Input.Fields["role"]
	if !ok {
		t.Fatal("admin.add-team input missing 'role' field")
	}
	if !strings.Contains(role.Description, "type: team") {
		t.Errorf("role description must document the type: team constraint, got %q", role.Description)
	}
}

// TestCommandSchema_AllCommandsHaveOutput asserts the schema invariant
// the json_schema command relies on: every entry MUST have an Output
// section (Input is optional for read-only commands). A nil Output
// would crash `conga json-schema --all` consumers expecting a stable
// shape.
func TestCommandSchema_AllCommandsHaveOutput(t *testing.T) {
	for name, schema := range commandSchemas {
		if schema.Output == nil {
			t.Errorf("commandSchemas[%q] has nil Output section — all schemas must declare their output shape", name)
		}
		if schema.Command == "" {
			t.Errorf("commandSchemas[%q] has empty Command field — required for human-readable listing", name)
		}
		if schema.Description == "" {
			t.Errorf("commandSchemas[%q] has empty Description — required for json-schema listing", name)
		}
	}
}
