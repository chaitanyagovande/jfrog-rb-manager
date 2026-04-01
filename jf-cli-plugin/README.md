# rb-manager — JFrog CLI Plugin

A [JFrog CLI plugin](https://docs.jfrog-applications.jfrog.io/jfrog-applications/jfrog-cli/cli-plugins) that creates **Release Bundle v2** from the latest builds matching an environment filter.

Runs as `jf rb-manager create` — fully non-interactive, native to the `jf` CLI, and CI-ready out of the box.

## Prerequisites

- [Go 1.22+](https://go.dev/dl/) (build-time only)
- [JFrog CLI](https://docs.jfrog-applications.jfrog.io/jfrog-applications/jfrog-cli/install) configured with a server (`jf config add`)

## Build & Install

```bash
cd jf-cli-plugin

# Resolve dependencies, build, and install into ~/.jfrog/plugins/
make install
```

After installation, verify with:

```bash
jf rb-manager --help
```

## Usage

```bash
# Single build
jf rb-manager create cg-webgoat-by

# Multiple builds
jf rb-manager create cg-webgoat-by cg-petclinic-by cg-juice-shop-by

# Full options
jf rb-manager create \
    --rb-name=prod-release \
    --rb-version=2.0.0 \
    --signing-key=my-gpg-key \
    --env-value=chaitanyagovande \
    --include-deps \
    cg-webgoat-by cg-petclinic-by

# Dry run — print payload without creating
jf rb-manager create --dry-run cg-webgoat-by
```

## Flags

| Flag | Description | Default |
|---|---|---|
| `--env-key` | Build property key to filter on | `buildInfo.env.GITHUB_REPOSITORY_OWNER` |
| `--env-value` | Build property value to match | `chaitanyagovande` |
| `--rb-name` | Release Bundle name | `my-release-bundle` |
| `--rb-version` | Release Bundle version | `1.0.0` |
| `--signing-key` | GPG signing key name | *(platform default)* |
| `--project` | JFrog project key | *(none)* |
| `--server-id` | JFrog CLI server config ID | *(default server)* |
| `--include-deps` | Include build dependencies | `false` |
| `--dry-run` | Print payload, skip creation | `false` |
| `--fail-on-missing` | Fail if any build name not found | `true` |

## CI / Pipeline Usage

The plugin is always non-interactive — no prompts. Missing builds are controlled by `--fail-on-missing`:

```yaml
# GitHub Actions example
- name: Install rb-manager plugin
  run: |
    cd jf-cli-plugin
    make install

- name: Create Release Bundle
  run: |
    jf rb-manager create \
      --rb-name=prod-release \
      --rb-version=${{ github.run_number }} \
      --signing-key=my-gpg-key \
      --fail-on-missing=true \
      cg-webgoat-by cg-petclinic-by
```

## How It Works

1. **AQL Query** — queries Artifactory for builds matching the environment filter.
2. **Latest Build Extraction** — groups by build name, picks most recent per name. Handles both `build.name` and `name` AQL field conventions.
3. **Payload Construction** — builds the Release Bundle v2 JSON payload using `jq`-free, type-safe Go structs.
4. **Release Bundle Creation** — POSTs to the Lifecycle API (`/lifecycle/api/v2/release_bundle`) with authentication from the JFrog CLI config.

## Input Validation

All user-supplied values are validated against `^[a-zA-Z0-9._-]+$` before use — build names, RB name/version, signing key, and project key. AQL injection is structurally prevented since Go's `fmt.Sprintf` with pre-validated inputs cannot produce query-breaking characters.

## Development

```bash
make test      # run tests
make lint      # run go vet
make build     # build binary without installing
make clean     # remove binary
```
