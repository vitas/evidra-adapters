package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vitas/evidra-adapters/terraform"
)

func main() {
	jsonErrors := false
	formatMode := "input"
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--version":
			fmt.Printf("evidra-adapter-terraform %s\n", terraform.Version)
			os.Exit(0)
		case "--json-errors":
			jsonErrors = true
		case "--format":
			if i+1 < len(args) {
				i++
				formatMode = args[i]
			} else {
				exitError(jsonErrors, "USAGE_ERROR", "--format requires a value (input or full)", "", 2)
			}
		case "--help", "-h":
			fmt.Fprintf(os.Stderr, "Usage: terraform show -json tfplan.bin | evidra-adapter-terraform [--format input|full] [--json-errors]\n")
			os.Exit(0)
		default:
			exitError(jsonErrors, "USAGE_ERROR", fmt.Sprintf("unknown flag: %s", args[i]), "", 2)
		}
	}
	if formatMode != "input" && formatMode != "full" {
		exitError(jsonErrors, "USAGE_ERROR",
			fmt.Sprintf("unknown format %q: must be 'input' or 'full'", formatMode), "", 2)
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		exitError(jsonErrors, "USAGE_ERROR", fmt.Sprintf("read stdin: %v", err), "", 2)
	}
	if len(raw) == 0 {
		exitError(jsonErrors, "EMPTY_INPUT", "empty input",
			"Pipe terraform show -json output to stdin", 2)
	}

	// Parse config from environment variables.
	config := map[string]string{}
	envKeys := []string{
		"filter_resource_types",
		"filter_actions",
		"include_data_sources",
		"max_resource_changes",
		"resource_changes_sort",
		"truncate_strategy",
	}
	for _, key := range envKeys {
		if v := os.Getenv("EVIDRA_" + strings.ToUpper(key)); v != "" {
			config[key] = v
		}
	}

	a := &terraform.PlanAdapter{}
	result, err := a.Convert(context.Background(), raw, config)
	if err != nil {
		code := "PARSE_ERROR"
		if strings.Contains(err.Error(), "validate") {
			code = "VALIDATION_ERROR"
		}
		exitError(jsonErrors, code, err.Error(),
			"Ensure input is from `terraform show -json`, not `terraform plan`", 1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	var output any
	if formatMode == "full" {
		output = result
	} else {
		output = result.Input
	}
	if err := enc.Encode(output); err != nil {
		exitError(jsonErrors, "PARSE_ERROR", fmt.Sprintf("encode result: %v", err), "", 1)
	}
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Hint           string `json:"hint,omitempty"`
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapter_version"`
}

func exitError(jsonMode bool, code, message, hint string, exitCode int) {
	if jsonMode {
		env := errorEnvelope{Error: errorDetail{
			Code:           code,
			Message:        message,
			Hint:           hint,
			Adapter:        "terraform-plan",
			AdapterVersion: terraform.Version,
		}}
		json.NewEncoder(os.Stderr).Encode(env) //nolint:errcheck
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", message)
		if hint != "" {
			fmt.Fprintf(os.Stderr, "hint: %s\n", hint)
		}
	}
	os.Exit(exitCode)
}
