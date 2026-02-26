package adapter_test

import (
	"context"
	"testing"

	"github.com/evidra/adapters/adapter"
)

// stubAdapter is a minimal implementation to verify the interface compiles.
type stubAdapter struct{}

var _ adapter.Adapter = (*stubAdapter)(nil)

func (s *stubAdapter) Name() string { return "stub" }

func (s *stubAdapter) Convert(_ context.Context, _ []byte, _ map[string]string) (*adapter.Result, error) {
	return &adapter.Result{
		Input:    map[string]any{"key": "value"},
		Metadata: map[string]any{"adapter_name": "stub"},
	}, nil
}

func TestAdapterInterface_Compliance(t *testing.T) {
	t.Parallel()

	var a adapter.Adapter = &stubAdapter{}
	if a.Name() != "stub" {
		t.Fatalf("expected name 'stub', got %q", a.Name())
	}

	result, err := a.Convert(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Input["key"] != "value" {
		t.Fatalf("expected input key 'value', got %v", result.Input["key"])
	}
}
