package terraform_test

import (
	"context"
	"testing"

	"github.com/vitas/evidra-adapters/terraform"
)

// TestAdapterOutputSatisfiesKillSwitch verifies that adapter output
// contains the fields Evidra kill-switch expects for terraform.apply.
func TestAdapterOutputSatisfiesKillSwitch(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "simple_create.json")
	result, err := (&terraform.PlanAdapter{}).Convert(
		context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Kill-switch terraform_has_detail() requires at least one of:
	// resource_types, s3_public_access_block, server_side_encryption,
	// security_group_rules, iam_policy_statements, trust_policy_statements
	//
	// v1 adapter provides resource_types. The rest are planned for v2.

	// resource_types: must exist, be array, len > 0
	types, ok := result.Input["resource_types"]
	if !ok {
		t.Fatal("resource_types missing — kill-switch will deny")
	}
	switch v := types.(type) {
	case []string:
		if len(v) == 0 {
			t.Fatal("resource_types empty — kill-switch will deny")
		}
	case []any:
		if len(v) == 0 {
			t.Fatal("resource_types empty — kill-switch will deny")
		}
	default:
		t.Fatalf("resource_types is %T, want []string or []any", types)
	}

	// destroy_count: must exist and be numeric
	dc, ok := result.Input["destroy_count"]
	if !ok {
		t.Fatal("destroy_count missing")
	}
	if _, isInt := dc.(int); !isInt {
		if _, isFloat := dc.(float64); !isFloat {
			t.Errorf("destroy_count is %T, want int or float64", dc)
		}
	}

	// truncation flags: must exist and be bool
	for _, flag := range []string{
		"resource_changes_truncated",
		"delete_addresses_truncated",
		"replace_addresses_truncated",
	} {
		v, ok := result.Input[flag]
		if !ok {
			t.Errorf("%s missing", flag)
			continue
		}
		if _, isBool := v.(bool); !isBool {
			t.Errorf("%s is %T, want bool", flag, v)
		}
	}

	// Additional kill-switch fields: counts must be present and numeric.
	for _, field := range []string{
		"create_count", "update_count", "replace_count", "total_changes",
	} {
		v, ok := result.Input[field]
		if !ok {
			t.Errorf("%s missing", field)
			continue
		}
		if _, isInt := v.(int); !isInt {
			if _, isFloat := v.(float64); !isFloat {
				t.Errorf("%s is %T, want int or float64", field, v)
			}
		}
	}

	// Boolean risk shortcuts must be present.
	for _, field := range []string{
		"has_destroys", "has_replaces", "is_destroy_plan",
	} {
		v, ok := result.Input[field]
		if !ok {
			t.Errorf("%s missing", field)
			continue
		}
		if _, isBool := v.(bool); !isBool {
			t.Errorf("%s is %T, want bool", field, v)
		}
	}

	// metadata.warnings must be present (reserved for v2).
	warnings, ok := result.Metadata["warnings"]
	if !ok {
		t.Fatal("metadata.warnings missing")
	}
	if _, isSlice := warnings.([]string); !isSlice {
		t.Errorf("metadata.warnings is %T, want []string", warnings)
	}
}
