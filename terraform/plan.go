package terraform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/vitas/evidra-adapters/adapter"
)

// Version is the adapter version, set at build time via ldflags.
var Version = "dev"

// Now is the time function used for timestamps. Override in tests.
var Now = time.Now

// OutputSchemaVersion is the output contract identifier.
// Bump only on breaking changes (field removal, semantic change).
const OutputSchemaVersion = "terraform-plan@v1"

const (
	defaultMaxResourceChanges = 200
	defaultSort               = "address"
	defaultTruncateStrategy   = "drop_tail"
)

// PlanAdapter converts `terraform show -json` output into Evidra skill input.
type PlanAdapter struct{}

var _ adapter.Adapter = (*PlanAdapter)(nil)

func (a *PlanAdapter) Name() string { return "terraform-plan" }

func (a *PlanAdapter) Convert(
	ctx context.Context, raw []byte, config map[string]string,
) (*adapter.Result, error) {
	var plan tfjson.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("terraform-plan: unmarshal: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("terraform-plan: validate: %w", err)
	}

	// --- Parse config ---
	includeData := config["include_data_sources"] == "true"
	filterTypes := parseCSV(config["filter_resource_types"])
	filterActions := parseCSV(config["filter_actions"])
	maxChanges := parseIntOrDefault(config["max_resource_changes"], defaultMaxResourceChanges)
	sortOrder := configOrDefault(config["resource_changes_sort"], defaultSort)
	truncateStrategy := configOrDefault(config["truncate_strategy"], defaultTruncateStrategy)

	// --- Single pass with two concerns ---
	//
	// SEMANTIC CONTRACT:
	//   filter_resource_types → filters EVERYTHING (counts, types, changes).
	//     Rationale: you asked "only look at hcloud_server" — counts should reflect that scope.
	//   filter_actions → filters ONLY the resource_changes array.
	//     Rationale: counts must reflect the full picture so policy can say
	//     "there are 5 deletes" even when resource_changes only shows creates.
	//   include_data_sources → filters EVERYTHING (data sources are not infra changes).
	//
	// This distinction matters: filter_resource_types narrows the *scope* of analysis,
	// while filter_actions narrows the *detail* you want to see. Counts always
	// reflect the full scope.

	var creates, updates, deletes, replaces int
	resourceTypes := map[string]bool{}
	providers := map[string]bool{}
	deleteTypes := map[string]bool{}
	replaceTypes := map[string]bool{}
	var deleteAddresses, replaceAddresses []string
	var changes []map[string]any

	for _, rc := range plan.ResourceChanges {
		if rc.Change == nil {
			continue
		}
		// Scope filters: exclude from everything.
		if rc.Mode == tfjson.DataResourceMode && !includeData {
			continue
		}
		if len(filterTypes) > 0 && !filterTypes[rc.Type] {
			continue
		}

		resourceTypes[rc.Type] = true
		if rc.ProviderName != "" {
			providers[rc.ProviderName] = true
		}

		action := primaryAction(rc.Change.Actions)

		// --- Always count (regardless of filter_actions) ---
		switch action {
		case "create":
			creates++
		case "update":
			updates++
		case "delete":
			deletes++
			deleteTypes[rc.Type] = true
			deleteAddresses = append(deleteAddresses, rc.Address)
		case "replace":
			replaces++
			replaceTypes[rc.Type] = true
			replaceAddresses = append(replaceAddresses, rc.Address)
		}

		// --- Detail filter: only affects resource_changes array ---
		if len(filterActions) > 0 && !filterActions[action] {
			continue
		}

		changes = append(changes, map[string]any{
			"address":  rc.Address,
			"type":     rc.Type,
			"action":   action,
			"provider": rc.ProviderName,
		})
	}

	// --- Sort (deterministic output) ---
	if sortOrder == "address" {
		sort.Slice(changes, func(i, j int) bool {
			return changes[i]["address"].(string) < changes[j]["address"].(string)
		})
		sort.Strings(deleteAddresses)
		sort.Strings(replaceAddresses)
	}

	// --- Truncate ---
	// Each array tracks its own total and truncated flag independently.
	// resource_changes is subject to filter_actions, so its total may differ
	// from total_changes. Address arrays are never filtered by filter_actions
	// (they come from counts), so they have their own totals.

	rcTotal := len(changes)
	rcTruncated := false
	if maxChanges >= 0 && rcTotal > maxChanges {
		rcTruncated = true
		if truncateStrategy == "summary_only" {
			changes = nil
		} else {
			changes = changes[:maxChanges]
		}
	}

	deleteAddrTotal := len(deleteAddresses)
	deleteAddrTruncated := false
	if maxChanges >= 0 && deleteAddrTotal > maxChanges {
		deleteAddrTruncated = true
		deleteAddresses = deleteAddresses[:maxChanges]
	}

	replaceAddrTotal := len(replaceAddresses)
	replaceAddrTruncated := false
	if maxChanges >= 0 && replaceAddrTotal > maxChanges {
		replaceAddrTruncated = true
		replaceAddresses = replaceAddresses[:maxChanges]
	}

	// --- Warnings ---
	var warnings []string

	if len(plan.ResourceChanges) == 0 {
		warnings = append(warnings, "plan contains no resource changes")
	}
	if plan.TerraformVersion == "" {
		warnings = append(warnings, "terraform_version missing from plan JSON")
	}
	if rcTruncated {
		warnings = append(warnings,
			fmt.Sprintf("resource_changes truncated: showing %d of %d",
				len(changes), rcTotal))
	}
	if deleteAddrTruncated {
		warnings = append(warnings,
			fmt.Sprintf("delete_addresses truncated: showing %d of %d",
				len(deleteAddresses), deleteAddrTotal))
	}
	if replaceAddrTruncated {
		warnings = append(warnings,
			fmt.Sprintf("replace_addresses truncated: showing %d of %d",
				len(replaceAddresses), replaceAddrTotal))
	}
	if len(plan.ResourceChanges) > 500 {
		warnings = append(warnings,
			fmt.Sprintf("large plan with %d resources; consider EVIDRA_FILTER_RESOURCE_TYPES",
				len(plan.ResourceChanges)))
	}
	if warnings == nil {
		warnings = []string{}
	}

	// --- Compose result ---
	isDestroyPlan := deletes > 0 && creates == 0 && updates == 0 && replaces == 0

	return &adapter.Result{
		Input: map[string]any{
			// Counts (always accurate within resource type scope)
			"create_count":  creates,
			"update_count":  updates,
			"destroy_count": deletes,
			"replace_count": replaces,
			"total_changes": creates + updates + deletes + replaces,

			// Classification
			"resource_types":  sortedKeys(resourceTypes),
			"providers":       sortedKeys(providers),
			"has_destroys":    deletes > 0,
			"has_replaces":    replaces > 0,
			"is_destroy_plan": isDestroyPlan,

			// NOTE: drift_count and deferred_count are NOT scope-filtered.
			// They reflect the entire plan regardless of filter_resource_types
			// or include_data_sources. This is intentional — drift in an
			// unfiltered resource type is still policy-relevant signal.
			"drift_count":    len(plan.ResourceDrift),
			"deferred_count": len(plan.DeferredChanges),

			// Risk shortcuts (not affected by filter_actions)
			"delete_types":                sortedKeys(deleteTypes),
			"replace_types":               sortedKeys(replaceTypes),
			"delete_addresses":            deleteAddresses,
			"delete_addresses_total":      deleteAddrTotal,
			"delete_addresses_truncated":  deleteAddrTruncated,
			"replace_addresses":           replaceAddresses,
			"replace_addresses_total":     replaceAddrTotal,
			"replace_addresses_truncated": replaceAddrTruncated,

			// Per-resource detail (subject to filter_actions + truncation)
			"resource_changes":           changes,
			"resource_changes_count":     rcTotal,
			"resource_changes_truncated": rcTruncated,
		},
		Metadata: map[string]any{
			"adapter_name":          "terraform-plan",
			"adapter_version":       Version,
			"output_schema_version": OutputSchemaVersion,
			"terraform_version":     plan.TerraformVersion,
			"format_version":        plan.FormatVersion,
			"resource_count":        len(plan.ResourceChanges),
			"timestamp":             Now().UTC().Format(time.RFC3339),
			"artifact_sha256":       sha256Hex(raw),
			"warnings":              warnings,
		},
	}, nil
}

// primaryAction maps tfjson.Actions to a single action string.
// Replace is detected via the compound [delete, create] or [create, delete] pattern.
func primaryAction(actions tfjson.Actions) string {
	if actions.Replace() {
		return "replace"
	}
	if actions.Create() {
		return "create"
	}
	if actions.Delete() {
		return "delete"
	}
	if actions.Update() {
		return "update"
	}
	if actions.Read() {
		return "read"
	}
	// Future-proofing: if Terraform adds new action types,
	// return "unknown" rather than silently mapping to "noop".
	if actions.NoOp() {
		return "noop"
	}
	return "unknown"
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func parseCSV(s string) map[string]bool {
	if s == "" {
		return nil
	}
	m := map[string]bool{}
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			m[v] = true
		}
	}
	return m
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseIntOrDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func configOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
