package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evidra/adapters/adapter"
)

func buildTestBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "evidra-adapter-terraform")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	return binary
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	// Fixtures live in terraform/testdata/, two levels up from cmd/evidra-adapter-terraform/.
	data, err := os.ReadFile(filepath.Join("..", "..", "terraform", "testdata", name))
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

func TestCLI_StdinStdout(t *testing.T) {
	binary := buildTestBinary(t)
	fixture := loadFixture(t, "simple_create.json")

	cmd := exec.Command(binary)
	cmd.Stdin = bytes.NewReader(fixture)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("command failed: %v\nstderr: %s", err, stderr.String())
	}

	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("stdout is not valid JSON: %s", stdout.String())
	}

	var result adapter.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	createCount, ok := result.Input["create_count"]
	if !ok {
		t.Fatal("missing create_count in output")
	}
	// JSON numbers unmarshal as float64
	if int(createCount.(float64)) != 2 {
		t.Errorf("expected create_count=2, got %v", createCount)
	}

	// stderr must be empty on success
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr output: %s", stderr.String())
	}
}

func TestCLI_EmptyStdin(t *testing.T) {
	binary := buildTestBinary(t)

	cmd := exec.Command(binary)
	cmd.Stdin = strings.NewReader("")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for empty stdin")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %v", err)
	}
	if !strings.Contains(stderr.String(), "empty input") {
		t.Errorf("expected 'empty input' in stderr, got: %s", stderr.String())
	}
}

func TestCLI_JsonErrors(t *testing.T) {
	binary := buildTestBinary(t)

	cmd := exec.Command(binary, "--json-errors")
	cmd.Stdin = strings.NewReader("{invalid}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid input")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %v", err)
	}

	// stdout must be empty on error
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on error, got: %s", stdout.String())
	}

	// stderr is valid JSON error envelope
	if !json.Valid(stderr.Bytes()) {
		t.Fatalf("stderr is not valid JSON: %s", stderr.String())
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
			Adapter string `json:"adapter"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error envelope: %v", err)
	}
	if env.Error.Code != "PARSE_ERROR" {
		t.Errorf("expected code PARSE_ERROR, got %q", env.Error.Code)
	}
	if env.Error.Adapter != "terraform-plan" {
		t.Errorf("expected adapter 'terraform-plan', got %q", env.Error.Adapter)
	}
	if env.Error.Hint == "" {
		t.Error("expected non-empty hint in error envelope")
	}
}

func TestCLI_EnvConfig(t *testing.T) {
	binary := buildTestBinary(t)
	fixture := loadFixture(t, "mixed_changes.json")

	cmd := exec.Command(binary)
	cmd.Stdin = bytes.NewReader(fixture)
	cmd.Env = append(os.Environ(),
		"EVIDRA_MAX_RESOURCE_CHANGES=2",
		"EVIDRA_TRUNCATE_STRATEGY=drop_tail",
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var result adapter.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	changes := result.Input["resource_changes"].([]any)
	if len(changes) > 2 {
		t.Errorf("expected <=2 resource_changes, got %d", len(changes))
	}

	truncated := result.Input["resource_changes_truncated"].(bool)
	if !truncated {
		t.Error("expected resource_changes_truncated=true")
	}
}

func TestCLI_Version(t *testing.T) {
	binary := buildTestBinary(t)

	var stdout bytes.Buffer
	cmd := exec.Command(binary, "--version")
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Fatalf("--version failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "evidra-adapter-terraform") {
		t.Errorf("expected version string to contain 'evidra-adapter-terraform', got: %s", stdout.String())
	}
}

func TestCLI_UnknownFlag(t *testing.T) {
	binary := buildTestBinary(t)

	var stderr bytes.Buffer
	cmd := exec.Command(binary, "--bogus")
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader("")

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown flag")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Errorf("expected exit code 2, got %v", err)
	}
}

func TestCLI_ValidationError_JsonErrors(t *testing.T) {
	binary := buildTestBinary(t)

	// Valid JSON but invalid plan (bad format_version).
	cmd := exec.Command(binary, "--json-errors")
	cmd.Stdin = strings.NewReader(`{"format_version":"99.0","terraform_version":"1.10.0","resource_changes":[]}`)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid plan")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %v", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout, got: %s", stdout.String())
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal error envelope: %v\nstderr: %s", err, stderr.String())
	}
	// terraform-json validates format_version during Unmarshal, so the error
	// is classified as PARSE_ERROR (not VALIDATION_ERROR).
	if env.Error.Code != "PARSE_ERROR" && env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("expected PARSE_ERROR or VALIDATION_ERROR, got %q", env.Error.Code)
	}
}

func TestCLI_Help(t *testing.T) {
	binary := buildTestBinary(t)

	var stderr bytes.Buffer
	cmd := exec.Command(binary, "--help")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("expected 'Usage' in help output, got: %s", stderr.String())
	}
}
