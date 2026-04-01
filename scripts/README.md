# JFrog Release Bundle Manager

Create **Release Bundle v2** from the latest builds matching an environment filter. Two scripts are provided — pick the one that fits your workflow.

## Prerequisites

- [JFrog CLI](https://docs.jfrog-applications.jfrog.io/jfrog-applications/jfrog-cli/install) configured with a server (`jf config add`)
- Admin access (required for AQL builds-domain queries)
- [`jq`](https://jqlang.github.io/jq/download/) installed

## Quick Start

Both scripts share the same interface:

```bash
# Single build — via REST API
./create-rb-via-api.sh cg-webgoat-by

# Multiple builds — via JFrog CLI
./create-rb-via-cli.sh cg-webgoat-by cg-petclinic-by cg-juice-shop-by
```

## Environment Variables

Override any default via environment variables:

| Variable | Description | Default |
|---|---|---|
| `ENV_KEY` | Build property key to filter on | `buildInfo.env.GITHUB_REPOSITORY_OWNER` |
| `ENV_VALUE` | Build property value to match | `chaitanyagovande` |
| `RB_NAME` | Release Bundle name | `my-release-bundle` |
| `RB_VERSION` | Release Bundle version | `1.0.0` |
| `SIGNING_KEY` | GPG signing key name | *(platform default)* |
| `INCLUDE_DEPS` | Include build dependencies (`true`/`false`) | `false` |
| `DRY_RUN` | Print payload/spec without creating (`true`/`false`) | `false` |
| `FAIL_ON_MISSING` | In CI mode: fail on missing builds (`true`/`false`) | `true` |
| `SYNC` | Wait for creation to complete — **CLI script only** (`true`/`false`) | `true` |
| `PROJECT_KEY` | JFrog project key — **CLI script only** | *(none)* |

### Example

```bash
RB_NAME="prod-release" \
RB_VERSION="2.0.0" \
SIGNING_KEY="my-gpg-key" \
ENV_VALUE="chaitanyagovande" \
./create-rb-via-api.sh cg-webgoat-by cg-petclinic-by
```

### Dry Run

Preview the payload or spec without creating anything:

```bash
DRY_RUN=true ./create-rb-via-cli.sh cg-webgoat-by
```

## CI / Non-Interactive Mode

Both scripts auto-detect non-interactive environments and disable all prompts. Detection triggers:

1. **`CI=true`** environment variable (set automatically by GitHub Actions, GitLab CI, Jenkins, etc.)
2. **stdin is not a TTY** (e.g. when piped or run by a scheduler)

When a requested build name is not found:

| Mode | Behavior |
|---|---|
| **Interactive** (local terminal) | Prompts "Continue without them? (y/N)" |
| **Non-interactive** + `FAIL_ON_MISSING=true` *(default)* | Exits with error — safe default for pipelines |
| **Non-interactive** + `FAIL_ON_MISSING=false` | Logs a warning and continues |

### GitHub Actions Examples

Using the **CLI script** (`jf rbc` under the hood):

```yaml
- name: Create Release Bundle (via jf rbc)
  env:
    RB_NAME: prod-release
    RB_VERSION: ${{ github.run_number }}
    SIGNING_KEY: my-gpg-key
    SYNC: "true"
    FAIL_ON_MISSING: "true"
  run: ./scripts/create-rb-via-cli.sh cg-webgoat-by cg-petclinic-by
```

Using the **REST API script** (direct HTTP call):

```yaml
- name: Create Release Bundle (via REST API)
  env:
    RB_NAME: prod-release
    RB_VERSION: ${{ github.run_number }}
    SIGNING_KEY: my-gpg-key
    FAIL_ON_MISSING: "true"
  run: ./scripts/create-rb-via-api.sh cg-webgoat-by cg-petclinic-by
```

## Script Comparison

| | REST API (`create-rb-via-api.sh`) | JFrog CLI (`create-rb-via-cli.sh`) |
|---|---|---|
| **Creates RB via** | `POST /lifecycle/api/v2/release_bundle` | `jf rbc --spec=...` |
| **Auth handling** | `jf rt curl` (inherits CLI config) | Native CLI auth |
| **Project support** | Add `project_key` to JSON payload | `--project` flag built in |
| **Sync / async** | Async by default (check status separately) | `--sync=true` waits for completion |
| **Best for** | CI/CD pipelines, custom integrations | Interactive use, simpler pipelines |

## Input Validation

Both scripts enforce strict input validation before executing any queries or API calls:

- **Build names, RB name/version, signing key, and project key** are restricted to alphanumeric characters, hyphens, underscores, and dots.
- **AQL injection prevention** — values interpolated into AQL queries are rejected if they contain `"`, `\`, `$`, `` ` ``, or `'`.
- **Boolean env vars** (`INCLUDE_DEPS`, `DRY_RUN`, `SYNC`, `FAIL_ON_MISSING`) must be exactly `true` or `false`.
- **API responses** are validated as proper JSON and checked for error payloads before processing.
- **Missing builds** are handled based on interactive/CI mode (see above).

## How It Works

1. **AQL Query** — finds all builds matching the environment filter and requested build names.
2. **Latest Build Extraction** — groups results by build name and picks the most recently created build for each.
3. **Payload / Spec Generation** — constructs the Release Bundle payload (API script) or a `jf rbc` spec file (CLI script).
4. **Release Bundle Creation** — sends the request or runs `jf rbc` (skipped in dry-run mode).
