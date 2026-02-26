package terraform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/evidra/adapters/terraform"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

// largePlanBytes generates a plan JSON with n managed resource creates.
func largePlanBytes(t *testing.T, n int) []byte {
	t.Helper()
	type change struct {
		Actions []string `json:"actions"`
		Before  any      `json:"before"`
		After   any      `json:"after"`
	}
	type rc struct {
		Address      string `json:"address"`
		Mode         string `json:"mode"`
		Type         string `json:"type"`
		Name         string `json:"name"`
		ProviderName string `json:"provider_name"`
		Change       change `json:"change"`
	}
	type plan struct {
		FormatVersion    string `json:"format_version"`
		TerraformVersion string `json:"terraform_version"`
		ResourceChanges  []rc   `json:"resource_changes"`
	}
	p := plan{
		FormatVersion:    "1.2",
		TerraformVersion: "1.10.0",
		ResourceChanges:  make([]rc, n),
	}
	for i := 0; i < n; i++ {
		p.ResourceChanges[i] = rc{
			Address:      fmt.Sprintf("null_resource.item_%04d", i),
			Mode:         "managed",
			Type:         "null_resource",
			Name:         fmt.Sprintf("item_%04d", i),
			ProviderName: "registry.terraform.io/hashicorp/null",
			Change:       change{Actions: []string{"create"}, Before: nil, After: map[string]any{}},
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal large plan: %v", err)
	}
	return b
}

func TestPlanAdapter_SimpleCreate(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "simple_create.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertInt(t, "create_count", 2, result.Input["create_count"])
	assertInt(t, "destroy_count", 0, result.Input["destroy_count"])
	assertInt(t, "update_count", 0, result.Input["update_count"])
	assertInt(t, "replace_count", 0, result.Input["replace_count"])
	assertInt(t, "total_changes", 2, result.Input["total_changes"])
	assertBool(t, "has_destroys", false, result.Input["has_destroys"])
	assertBool(t, "has_replaces", false, result.Input["has_replaces"])
	assertBool(t, "is_destroy_plan", false, result.Input["is_destroy_plan"])
	assertBool(t, "resource_changes_truncated", false, result.Input["resource_changes_truncated"])
	assertInt(t, "resource_changes_count", 2, result.Input["resource_changes_count"])

	// Metadata
	assertStr(t, "adapter_name", "terraform-plan", result.Metadata["adapter_name"])
	assertStr(t, "output_schema_version", "terraform-plan@v1", result.Metadata["output_schema_version"])
}

func TestPlanAdapter_EmptyPlan(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "empty_plan.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertInt(t, "total_changes", 0, result.Input["total_changes"])
	assertBool(t, "resource_changes_truncated", false, result.Input["resource_changes_truncated"])
	assertInt(t, "resource_changes_count", 0, result.Input["resource_changes_count"])

	deleteTypes := result.Input["delete_types"].([]string)
	if len(deleteTypes) != 0 {
		t.Errorf("expected empty delete_types, got %v", deleteTypes)
	}
	replaceTypes := result.Input["replace_types"].([]string)
	if len(replaceTypes) != 0 {
		t.Errorf("expected empty replace_types, got %v", replaceTypes)
	}
}

func TestPlanAdapter_DestroyOnly(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "destroy_only.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBool(t, "is_destroy_plan", true, result.Input["is_destroy_plan"])
	assertBool(t, "has_destroys", true, result.Input["has_destroys"])

	destroyCount := result.Input["destroy_count"].(int)
	if destroyCount <= 0 {
		t.Errorf("expected destroy_count > 0, got %d", destroyCount)
	}

	// Risk shortcuts populated
	deleteTypes := result.Input["delete_types"].([]string)
	if len(deleteTypes) == 0 {
		t.Error("expected non-empty delete_types")
	}
	deleteAddrs := result.Input["delete_addresses"].([]string)
	if len(deleteAddrs) == 0 {
		t.Error("expected non-empty delete_addresses")
	}
	// delete_addresses_total equals destroy_count
	assertInt(t, "delete_addresses_total", destroyCount, result.Input["delete_addresses_total"])
}

func TestPlanAdapter_MixedChanges(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// mixed_changes.json: 1 create, 1 update, 1 delete, 1 replace
	assertInt(t, "create_count", 1, result.Input["create_count"])
	assertInt(t, "update_count", 1, result.Input["update_count"])
	assertInt(t, "destroy_count", 1, result.Input["destroy_count"])
	assertInt(t, "replace_count", 1, result.Input["replace_count"])
	assertInt(t, "total_changes", 4, result.Input["total_changes"])
	assertBool(t, "has_destroys", true, result.Input["has_destroys"])
	assertBool(t, "has_replaces", true, result.Input["has_replaces"])
	assertBool(t, "is_destroy_plan", false, result.Input["is_destroy_plan"])
}

func TestPlanAdapter_RiskShortcuts(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deleteTypes := result.Input["delete_types"].([]string)
	if len(deleteTypes) == 0 {
		t.Error("expected non-empty delete_types")
	}
	for i := 1; i < len(deleteTypes); i++ {
		if deleteTypes[i-1] > deleteTypes[i] {
			t.Errorf("delete_types not sorted: %v", deleteTypes)
		}
	}

	replaceTypes := result.Input["replace_types"].([]string)
	if len(replaceTypes) == 0 {
		t.Error("expected non-empty replace_types")
	}
	for i := 1; i < len(replaceTypes); i++ {
		if replaceTypes[i-1] > replaceTypes[i] {
			t.Errorf("replace_types not sorted: %v", replaceTypes)
		}
	}

	// replace_addresses_total equals replace_count
	assertInt(t, "replace_addresses_total", result.Input["replace_count"].(int), result.Input["replace_addresses_total"])
	assertInt(t, "delete_addresses_total", result.Input["destroy_count"].(int), result.Input["delete_addresses_total"])
}

func TestPlanAdapter_Truncation_DropTail(t *testing.T) {
	t.Parallel()

	raw := largePlanBytes(t, 500)
	a := &terraform.PlanAdapter{}
	config := map[string]string{"max_resource_changes": "10"}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	changes := result.Input["resource_changes"].([]map[string]any)
	if len(changes) != 10 {
		t.Errorf("expected 10 changes, got %d", len(changes))
	}
	assertBool(t, "resource_changes_truncated", true, result.Input["resource_changes_truncated"])

	rcCount := result.Input["resource_changes_count"].(int)
	if rcCount <= 10 {
		t.Errorf("resource_changes_count should be >10, got %d", rcCount)
	}

	// Counts remain accurate (computed before truncation)
	total := result.Input["create_count"].(int) + result.Input["update_count"].(int) +
		result.Input["destroy_count"].(int) + result.Input["replace_count"].(int)
	assertInt(t, "total_changes", total, result.Input["total_changes"])
}

func TestPlanAdapter_Truncation_SummaryOnly(t *testing.T) {
	t.Parallel()

	raw := largePlanBytes(t, 500)
	a := &terraform.PlanAdapter{}
	config := map[string]string{
		"max_resource_changes": "10",
		"truncate_strategy":    "summary_only",
	}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// resource_changes should be nil or empty with summary_only.
	// Use len() to avoid the typed-nil interface comparison gotcha.
	changes, _ := result.Input["resource_changes"].([]map[string]any)
	if len(changes) != 0 {
		t.Errorf("expected empty resource_changes with summary_only, got %d entries", len(changes))
	}
	assertBool(t, "resource_changes_truncated", true, result.Input["resource_changes_truncated"])
	if result.Input["total_changes"].(int) == 0 {
		t.Error("expected total_changes > 0")
	}
}

func TestPlanAdapter_Sorting_Deterministic(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}

	r1, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("first convert: %v", err)
	}
	r2, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("second convert: %v", err)
	}

	c1 := r1.Input["resource_changes"].([]map[string]any)
	c2 := r2.Input["resource_changes"].([]map[string]any)
	if len(c1) != len(c2) {
		t.Fatalf("length mismatch: %d vs %d", len(c1), len(c2))
	}
	for i := range c1 {
		if c1[i]["address"] != c2[i]["address"] {
			t.Errorf("index %d: address mismatch %q vs %q", i, c1[i]["address"], c2[i]["address"])
		}
	}

	// Verify sorted by address
	for i := 1; i < len(c1); i++ {
		prev := c1[i-1]["address"].(string)
		curr := c1[i]["address"].(string)
		if prev > curr {
			t.Errorf("not sorted at index %d: %q > %q", i, prev, curr)
		}
	}
}

func TestPlanAdapter_Sorting_None(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}
	config := map[string]string{"resource_changes_sort": "none"}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Input["resource_changes"] == nil {
		t.Error("expected non-nil resource_changes")
	}
}

func TestPlanAdapter_FilterResourceTypes(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}
	config := map[string]string{"filter_resource_types": "hcloud_server"}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	types := result.Input["resource_types"].([]string)
	if len(types) != 1 || types[0] != "hcloud_server" {
		t.Errorf("expected [hcloud_server], got %v", types)
	}
}

func TestPlanAdapter_FilterActions_DoesNotAffectCounts(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "mixed_changes.json")
	a := &terraform.PlanAdapter{}

	// Without filter: get baseline counts
	baseline, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("baseline convert: %v", err)
	}
	baselineDeletes := baseline.Input["destroy_count"].(int)
	baselineTotal := baseline.Input["total_changes"].(int)

	// With filter_actions=create: only show creates in resource_changes
	filtered, err := a.Convert(context.Background(), raw, map[string]string{"filter_actions": "create"})
	if err != nil {
		t.Fatalf("filtered convert: %v", err)
	}

	// Counts MUST be identical — filter_actions is detail-only
	if filtered.Input["destroy_count"].(int) != baselineDeletes {
		t.Errorf("filter_actions must not affect destroy_count: got %d, want %d",
			filtered.Input["destroy_count"], baselineDeletes)
	}
	if filtered.Input["total_changes"].(int) != baselineTotal {
		t.Errorf("filter_actions must not affect total_changes: got %d, want %d",
			filtered.Input["total_changes"], baselineTotal)
	}

	// But resource_changes only contains creates
	changes := filtered.Input["resource_changes"].([]map[string]any)
	for _, c := range changes {
		if c["action"] != "create" {
			t.Errorf("expected only creates in resource_changes, got action %q", c["action"])
		}
	}

	// delete_types still populated (not affected by filter_actions)
	baselineDeleteTypes := baseline.Input["delete_types"].([]string)
	filteredDeleteTypes := filtered.Input["delete_types"].([]string)
	if len(baselineDeleteTypes) != len(filteredDeleteTypes) {
		t.Errorf("delete_types should not change with filter_actions: baseline=%v filtered=%v",
			baselineDeleteTypes, filteredDeleteTypes)
	}
}

func TestPlanAdapter_InvalidJSON(t *testing.T) {
	t.Parallel()

	a := &terraform.PlanAdapter{}
	_, err := a.Convert(context.Background(), []byte("{broken"), nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !containsStr(err.Error(), "unmarshal") {
		t.Errorf("expected 'unmarshal' in error, got: %v", err)
	}
}

func TestPlanAdapter_WithDrift(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "with_drift.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	driftCount := result.Input["drift_count"].(int)
	if driftCount != 1 {
		t.Errorf("expected drift_count=1, got %d", driftCount)
	}
}

func TestPlanAdapter_WithDeferred(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "with_deferred.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deferredCount := result.Input["deferred_count"].(int)
	if deferredCount != 1 {
		t.Errorf("expected deferred_count=1, got %d", deferredCount)
	}
}

func TestPlanAdapter_DataSources_ExcludedByDefault(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "data_sources.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// data_sources.json: 1 managed create + 2 data source reads
	// Data sources excluded by default.
	assertInt(t, "total_changes", 1, result.Input["total_changes"])
	assertInt(t, "create_count", 1, result.Input["create_count"])

	types := result.Input["resource_types"].([]string)
	for _, typ := range types {
		if typ == "hcloud_image" || typ == "hcloud_ssh_key" {
			t.Errorf("data source type %q should be excluded by default", typ)
		}
	}
}

func TestPlanAdapter_DataSources_Included(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "data_sources.json")
	a := &terraform.PlanAdapter{}
	config := map[string]string{"include_data_sources": "true"}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// data_sources.json: 1 managed create + 2 data source reads.
	// total_changes only counts create/update/delete/replace — reads are not infra changes.
	// With include_data_sources=true, data sources appear in resource_changes (detail view)
	// but not in the count totals.
	assertInt(t, "total_changes", 1, result.Input["total_changes"])
	assertInt(t, "resource_changes_count", 3, result.Input["resource_changes_count"])
}

func TestPlanAdapter_WithModules(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "with_modules.json")
	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// with_modules.json: 3 creates (1 root + 2 module)
	assertInt(t, "total_changes", 3, result.Input["total_changes"])
	assertInt(t, "create_count", 3, result.Input["create_count"])
}

func TestPlanAdapter_Metadata(t *testing.T) {
	// NOT parallel — modifies package-level terraform.Now.
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	origNow := terraform.Now
	terraform.Now = func() time.Time { return fixedTime }
	defer func() { terraform.Now = origNow }()

	raw := loadFixture(t, "simple_create.json")
	result, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertStr(t, "timestamp", "2026-01-01T00:00:00Z", result.Metadata["timestamp"])
	assertStr(t, "output_schema_version", "terraform-plan@v1", result.Metadata["output_schema_version"])
	assertStr(t, "adapter_name", "terraform-plan", result.Metadata["adapter_name"])

	sha := result.Metadata["artifact_sha256"].(string)
	if len(sha) != 64 {
		t.Errorf("expected 64-char sha256 hex, got %q (len %d)", sha, len(sha))
	}
}

func TestPlanAdapter_Metadata_ResourceCount(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "simple_create.json")
	result, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resourceCount := result.Metadata["resource_count"].(int)
	if resourceCount != 2 {
		t.Errorf("expected resource_count=2, got %d", resourceCount)
	}
}

func TestPlanAdapter_TotalChangesInvariant(t *testing.T) {
	t.Parallel()

	fixtures := []string{"simple_create.json", "mixed_changes.json", "destroy_only.json", "empty_plan.json"}
	for _, fix := range fixtures {
		fix := fix
		t.Run(fix, func(t *testing.T) {
			t.Parallel()
			raw := loadFixture(t, fix)
			result, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			creates := result.Input["create_count"].(int)
			updates := result.Input["update_count"].(int)
			deletes := result.Input["destroy_count"].(int)
			replaces := result.Input["replace_count"].(int)
			total := result.Input["total_changes"].(int)
			if total != creates+updates+deletes+replaces {
				t.Errorf("total_changes invariant violated: %d != %d+%d+%d+%d",
					total, creates, updates, deletes, replaces)
			}
		})
	}
}

func TestPlanAdapter_UnknownConfig_Ignored(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "simple_create.json")
	config := map[string]string{
		"unknown_key_future_version": "some_value",
		"another_unknown":            "42",
	}
	_, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unknown config keys should be silently ignored, got error: %v", err)
	}
}

func TestPlanAdapter_Name(t *testing.T) {
	t.Parallel()

	a := &terraform.PlanAdapter{}
	if a.Name() != "terraform-plan" {
		t.Errorf("expected name 'terraform-plan', got %q", a.Name())
	}
}

// largePlanDriftCount verifies that drift_count is not scope-filtered.
func TestPlanAdapter_DriftCount_NotScopeFiltered(t *testing.T) {
	t.Parallel()

	// with_drift.json has 1 create (hcloud_firewall) + 1 drift (hcloud_server).
	// Filter to only hcloud_firewall — drift should still be visible.
	raw := loadFixture(t, "with_drift.json")
	a := &terraform.PlanAdapter{}
	config := map[string]string{"filter_resource_types": "hcloud_firewall"}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// scope filter: only hcloud_firewall visible in counts
	assertInt(t, "create_count", 1, result.Input["create_count"])

	// drift_count is NOT scope-filtered — still 1
	assertInt(t, "drift_count", 1, result.Input["drift_count"])
}

// TestPlanAdapter_ResourceChanges_JSON verifies that resource_changes serialises
// correctly to JSON (used by CLI and downstream consumers).
func TestPlanAdapter_ResourceChanges_JSON(t *testing.T) {
	t.Parallel()

	raw := loadFixture(t, "simple_create.json")
	result, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	inputMap := decoded["input"].(map[string]any)
	changes := inputMap["resource_changes"].([]any)
	if len(changes) != 2 {
		t.Errorf("expected 2 resource_changes in JSON output, got %d", len(changes))
	}
}

// TestPlanAdapter_InvalidPlan_BadFormatVersion ensures that an otherwise valid
// JSON document that fails plan.Validate() returns a validation error.
func TestPlanAdapter_InvalidPlan_BadFormatVersion(t *testing.T) {
	t.Parallel()

	// format_version "99.0" is not supported by terraform-json.
	raw := []byte(`{"format_version":"99.0","terraform_version":"1.10.0","resource_changes":[]}`)
	_, err := (&terraform.PlanAdapter{}).Convert(context.Background(), raw, nil)
	if err == nil {
		t.Fatal("expected error for unsupported format_version")
	}
	// terraform-json validates format_version during Unmarshal, so the error
	// may say "unmarshal" or "validate" depending on the library version.
	if !containsStr(err.Error(), "format") && !containsStr(err.Error(), "unsupported") &&
		!containsStr(err.Error(), "validate") {
		t.Errorf("expected format/version error, got: %v", err)
	}
}

// --- helpers ---

func assertInt(t *testing.T, field string, want int, got any) {
	t.Helper()
	v, ok := got.(int)
	if !ok {
		t.Errorf("%s: expected int, got %T(%v)", field, got, got)
		return
	}
	if v != want {
		t.Errorf("%s: expected %d, got %d", field, want, v)
	}
}

func assertBool(t *testing.T, field string, want bool, got any) {
	t.Helper()
	v, ok := got.(bool)
	if !ok {
		t.Errorf("%s: expected bool, got %T(%v)", field, got, got)
		return
	}
	if v != want {
		t.Errorf("%s: expected %v, got %v", field, want, v)
	}
}

func assertStr(t *testing.T, field, want string, got any) {
	t.Helper()
	v, ok := got.(string)
	if !ok {
		t.Errorf("%s: expected string, got %T(%v)", field, got, got)
		return
	}
	if v != want {
		t.Errorf("%s: expected %q, got %q", field, want, v)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Compile-time check: PlanAdapter satisfies adapter.Adapter.
// Actual interface is imported in plan.go via var _ adapter.Adapter = (*PlanAdapter)(nil).
// This test imports tfjson to confirm the dependency chain compiles.
var _ = tfjson.Plan{}
