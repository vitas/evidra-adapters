// Package adapter defines the contract for Evidra input adapters.
//
// An adapter converts tool-specific output (terraform plan JSON,
// Kubernetes manifests, etc.) into business parameters that match
// an Evidra skill's input_schema.
package adapter

import "context"

// Result is the adapter output.
type Result struct {
	// Input contains extracted business parameters.
	// Keys and types must match the target skill's input_schema.
	// Example: {"destroy_count": 5, "resource_types": ["hcloud_server"]}
	Input map[string]any `json:"input"`

	// Metadata contains provenance information for audit logging.
	// It is NOT sent to the Evidra skill — callers may include it
	// in the actor.origin or as a separate audit record.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Adapter converts raw tool output into Evidra skill input.
type Adapter interface {
	// Name returns the adapter identifier.
	// Convention: "{tool}-{artifact}", e.g. "terraform-plan", "k8s-manifest".
	Name() string

	// Convert processes raw artifact bytes and extracts business parameters.
	//
	// Parameters:
	//   - ctx: cancellation/timeout context
	//   - raw: the artifact bytes (e.g. output of `terraform show -json`)
	//   - config: adapter-specific settings passed by the caller
	//
	// Returns:
	//   - *Result: extracted parameters + metadata
	//   - error: parse failure, unsupported format version, etc.
	Convert(ctx context.Context, raw []byte, config map[string]string) (*Result, error)
}
