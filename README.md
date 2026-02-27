# evidra-adapter-terraform

Transforms `terraform show -json` output into structured parameters for Evidra policy evaluation.

The adapter reads a Terraform plan JSON on stdin and outputs a structured `{"input": {...}, "metadata": {...}}` object on stdout. The `input` fields map directly to an Evidra skill's `input_schema` and can be POSTed to `/v1/validate` for policy evaluation — without sending raw plan data to Evidra.

## Install

**Binary (Linux/macOS):**
```bash
curl -fsSL https://github.com/evidra/adapters/releases/download/v0.1.0/evidra-adapter-terraform_v0.1.0_linux_amd64.tar.gz \
  | tar xz -C /usr/local/bin
```

**Docker:**
```bash
docker pull ghcr.io/evidra/adapter-terraform:v0.1.0
```

**Go:**
```bash
go install github.com/evidra/adapters/cmd/evidra-adapter-terraform@v0.1.0
```

## Usage

```bash
# Basic — inspect the output
terraform show -json tfplan.bin | evidra-adapter-terraform | jq .

# With Evidra API — full validation workflow
terraform show -json tfplan.bin | evidra-adapter-terraform \
  | jq -r '.input' \
  | curl -sf -X POST https://api.evidra.rest/v1/validate \
      -H "Authorization: Bearer $EVIDRA_API_KEY" \
      -H "Content-Type: application/json" \
      -d @-

# Docker
terraform show -json tfplan.bin \
  | docker run -i ghcr.io/evidra/adapter-terraform:v0.1.0 \
  | jq .input.destroy_count

# With structured errors for CI
terraform show -json tfplan.bin | evidra-adapter-terraform --json-errors
```

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

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success — valid JSON on stdout |
| `1` | Parse or validation error — bad input data |
| `2` | Usage error — empty stdin, unknown flag |

Use `--json-errors` to get a machine-readable JSON error envelope on stderr instead of plain text.

## CI Example

```yaml
- name: Install Evidra adapter
  run: |
    curl -fsSL https://github.com/evidra/adapters/releases/download/v0.1.0/evidra-adapter-terraform_v0.1.0_linux_amd64.tar.gz \
      | tar xz -C /usr/local/bin

- name: Evidra policy check
  run: |
    terraform show -json tfplan.bin \
      | evidra-adapter-terraform --json-errors \
      | jq -r '.input' \
      | curl -sf -X POST https://api.evidra.rest/v1/validate \
          -H "Authorization: Bearer ${{ secrets.EVIDRA_API_KEY }}" \
          -H "Content-Type: application/json" \
          -d @-
```

## Releasing

`git tag v0.2.0 && git push origin v0.2.0` — that's it. See [docs/releasing.md](docs/releasing.md) for the full process, pre-release checklist, and release artifacts.

## License

Apache-2.0. See [LICENSE](LICENSE).
