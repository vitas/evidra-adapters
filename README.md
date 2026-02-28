# evidra-adapter-terraform

Terraform Plan Metadata Adapter (v1) for Evidra.

Reads `terraform show -json` on stdin, extracts structured plan metadata, and outputs JSON on stdout for Evidra policy evaluation — without sending raw plan data to Evidra.

## Install

**Binary (Linux/macOS):**
```bash
curl -fsSL https://github.com/vitas/evidra-adapters/releases/download/v0.1.0/evidra-adapter-terraform_v0.1.0_linux_amd64.tar.gz \
  | tar xz -C /usr/local/bin
```

**Docker:**
```bash
docker pull ghcr.io/vitas/evidra-adapter-terraform:v0.1.0
```

**Go:**
```bash
go install github.com/vitas/evidra-adapters/cmd/evidra-adapter-terraform@v0.1.0
```

## Usage

```bash
# Basic — pipe to Evidra
terraform show -json tfplan.bin \
  | evidra-adapter-terraform \
  | curl -sf -X POST https://api.evidra.rest/v1/validate \
      -H "Authorization: Bearer $EVIDRA_API_KEY" \
      -H "Content-Type: application/json" \
      -d @-

# Inspect adapter output
terraform show -json tfplan.bin | evidra-adapter-terraform | jq .

# Full output (input + metadata) for debugging
terraform show -json tfplan.bin | evidra-adapter-terraform --format full | jq .

# Docker
terraform show -json tfplan.bin \
  | docker run -i ghcr.io/vitas/evidra-adapter-terraform:v0.1.0 \
  | jq .destroy_count

# With structured errors for CI
terraform show -json tfplan.bin | evidra-adapter-terraform --json-errors
```

By default, the adapter outputs only the `input` object (the payload Evidra expects). Use `--format full` to include the `metadata` wrapper for debugging.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `EVIDRA_FILTER_RESOURCE_TYPES` | (none) | Comma-separated resource types to include (e.g. `hcloud_server,hcloud_volume`) |
| `EVIDRA_FILTER_ACTIONS` | (none) | Comma-separated actions to include in `resource_changes` (e.g. `create,delete`) |
| `EVIDRA_INCLUDE_DATA_SOURCES` | `false` | Include data source reads in output |
| `EVIDRA_MAX_RESOURCE_CHANGES` | `200` | Max entries in `resource_changes`, `delete_addresses`, `replace_addresses` arrays |
| `EVIDRA_RESOURCE_CHANGES_SORT` | `address` | Sort order for `resource_changes`: `address` (deterministic) or `none` (plan order) |
| `EVIDRA_TRUNCATE_STRATEGY` | `drop_tail` | How to cap `resource_changes` when over limit: `drop_tail` (keep first N) or `summary_only` (emit empty array) |

**Important:** `EVIDRA_FILTER_RESOURCE_TYPES` is a scope filter — it narrows counts, types, and all arrays. `EVIDRA_FILTER_ACTIONS` is a detail filter — it only affects the `resource_changes` array and never changes counts like `destroy_count`.

## Output

The output has three tiers:

**Counts** — always accurate, never truncated, not affected by `EVIDRA_FILTER_ACTIONS`:
`create_count`, `update_count`, `destroy_count`, `replace_count`, `total_changes`, `drift_count`, `deferred_count`

**Risk shortcuts** — pre-computed fields that eliminate iteration in policy rules:
`has_destroys`, `has_replaces`, `is_destroy_plan`, `delete_types`, `replace_types`, `delete_addresses`, `replace_addresses` (with `_total` and `_truncated` variants)

**Per-resource detail** — subject to all filters and truncation:
`resource_changes[]` (each has `address`, `type`, `action`, `provider`), `resource_changes_count`, `resource_changes_truncated`

Full schema: see [adapter system design doc](docs/evidra_adapter_system_design.md).

## Output Contract (v1)

| field | type | always present | used by |
|---|---|---|---|
| `resource_types` | `string[]` | yes (may be empty) | kill-switch `terraform_has_detail` |
| `destroy_count` | `int` | yes | mass delete guard, `terraform.destroy` gate |
| `create_count` | `int` | yes | informational |
| `update_count` | `int` | yes | informational |
| `replace_count` | `int` | yes | informational |
| `total_changes` | `int` | yes | `= create + update + destroy + replace` |
| `has_destroys` | `bool` | yes | risk shortcut |
| `is_destroy_plan` | `bool` | yes | destroys only, no creates/updates |
| `resource_changes_truncated` | `bool` | yes | truncation guard |
| `delete_addresses_truncated` | `bool` | yes | truncation guard |
| `replace_addresses_truncated` | `bool` | yes | truncation guard |
| `delete_addresses` | `string[]` | yes (may be empty) | risk shortcut |
| `replace_addresses` | `string[]` | yes (may be empty) | risk shortcut |
| `drift_count` | `int` | yes | informational (not scope-filtered) |
| `deferred_count` | `int` | yes | informational (not scope-filtered) |

Fields NOT present in v1 (planned for v2):
`security_group_rules`, `iam_policy_statements`, `trust_policy_statements`,
`s3_public_access_block`, `server_side_encryption`.

## Coverage

**v1 (current) — plan metadata only.**

Extracts: counts, resource types, addresses, providers, drift,
deferred changes, truncation flags. Sufficient for kill-switch rules
(fail-closed, unknown tools, mass delete, truncation guard).

Does NOT extract resource-specific configuration:
- Security group rules (ingress/egress CIDR, ports)
- IAM policy statements (Action, Resource, Principal)
- S3 public access block / encryption settings

Ops-layer rules that inspect these fields (`deny_sg_open_world`,
`deny_terraform_iam_wildcard`, `deny_s3_public_access`) will not fire
in CI-only mode with v1 adapter.

**v2 (planned) — deep extraction.**

Will extract security group rules, IAM statements, and S3 config
from `resource_changes[].change.after` for supported AWS resource types.

**CI behavior in ops profile:**

In ops profile, `terraform.apply` with v1 adapter output (metadata-only)
is **denied by design**. The rule `ops.terraform_metadata_only` fires
because ops-layer rules need resource-specific fields that v1 adapter
does not extract.

Options:
1. **Baseline profile** — kill-switch only CI (counts, types, truncation)
2. **MCP mode** — AI agent extracts resource config into payload
3. **Adapter v2** — deep extraction (planned)

Example with baseline profile:
```bash
terraform show -json tfplan \
  | evidra-adapter-terraform \
  | EVIDRA_ENVIRONMENT=ci-baseline evidra validate --tool terraform --op apply
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success — valid JSON on stdout |
| `1` | Parse or validation error — bad input data |
| `2` | Usage error — empty stdin, unknown flag |

Use `--json-errors` to get a machine-readable JSON error envelope on stderr instead of plain text.

## Example output (after evidra validate)

```json
{
    "pass": true,
    "risk_level": "low",
    "rule_ids": [],
    "reasons": [],
    "action_results": [
        {
            "index": 0,
            "kind": "terraform.apply",
            "pass": true,
            "risk_level": "low",
            "rule_ids": [],
            "reasons": [],
            "hints": []
        }
    ]
}
```

When denied (e.g. metadata-only in ops profile):

```json
{
    "pass": false,
    "risk_level": "high",
    "rule_ids": ["ops.terraform_metadata_only"],
    "action_results": [
        {
            "index": 0,
            "kind": "terraform.apply",
            "pass": false,
            "rule_ids": ["ops.terraform_metadata_only"],
            "reasons": ["terraform.apply payload contains only plan metadata..."],
            "hints": [
                "Use MCP mode, adapter v2, or baseline profile.",
                "{\"payload\":{\"resource_types\":[\"...\"]}}"
            ]
        }
    ]
}
```

Note: `rule_ids` and `reasons` at top level are summary (deduped union).
Per-action details are in `action_results[]`.

## One plan, one run

The adapter processes one terraform plan per invocation.
For multiple plans, run the adapter separately for each:

```bash
for plan in plan1.json plan2.json; do
    cat "$plan" | evidra-adapter-terraform | evidra validate --tool terraform --op apply
done
```

## CI Example

```yaml
- name: Install Evidra adapter
  run: |
    curl -fsSL https://github.com/vitas/evidra-adapters/releases/download/v0.1.0/evidra-adapter-terraform_v0.1.0_linux_amd64.tar.gz \
      | tar xz -C /usr/local/bin

- name: Evidra policy check
  run: |
    terraform show -json tfplan.bin \
      | evidra-adapter-terraform --json-errors \
      | curl -sf -X POST https://api.evidra.rest/v1/validate \
          -H "Authorization: Bearer ${{ secrets.EVIDRA_API_KEY }}" \
          -H "Content-Type: application/json" \
          -d @-
```

## Releasing

`git tag v0.1.0 && git push origin v0.1.0` — that's it. See [docs/releasing.md](docs/releasing.md) for the full process, pre-release checklist, and release artifacts.

## License

Apache-2.0. See [LICENSE](LICENSE).
