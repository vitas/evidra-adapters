# Releasing

Releases are fully automated via GitHub Actions + goreleaser. There is no manual build step.

## Publishing a release

```bash
git tag v0.2.0
git push origin v0.2.0
```

That's it. Pushing a `v*` tag triggers `.github/workflows/release.yml`, which:

1. Runs goreleaser
2. Cross-compiles for linux/darwin/windows × amd64/arm64
3. Creates a GitHub Release with the binaries as `.tar.gz` archives
4. Publishes `checksums.txt` alongside the archives
5. Injects the tag as the version string (visible via `evidra-adapter-terraform --version`)

## Versioning convention

[SemVer](https://semver.org). The output schema version (`terraform-plan@v1` in metadata) is independent — it only bumps on breaking output changes, not on every release.

## Pre-release checklist

```bash
make test          # all tests pass, no race conditions
make lint          # go vet + gofmt clean
make smoke         # binary runs correctly against fixtures
go mod tidy        # go.sum up to date
```

## Release artifacts

| Artifact | Description |
|---|---|
| `evidra-adapter-terraform_vX.Y.Z_linux_amd64.tar.gz` | Linux x86-64 |
| `evidra-adapter-terraform_vX.Y.Z_linux_arm64.tar.gz` | Linux ARM64 |
| `evidra-adapter-terraform_vX.Y.Z_darwin_amd64.tar.gz` | macOS Intel |
| `evidra-adapter-terraform_vX.Y.Z_darwin_arm64.tar.gz` | macOS Apple Silicon |
| `evidra-adapter-terraform_vX.Y.Z_windows_amd64.tar.gz` | Windows x86-64 |
| `checksums.txt` | SHA-256 checksums for all archives |
