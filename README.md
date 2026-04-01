# JFrog Release Bundle Manager

Create **Release Bundle v2** from the latest builds matching an environment filter. Two implementations are provided — choose based on your needs.

## Project Structure

```
├── scripts/             Shell scripts — zero build step, works anywhere bash runs
│   ├── create-rb-via-api.sh   (creates RB via REST API)
│   ├── create-rb-via-cli.sh   (creates RB via jf rbc CLI)
│   └── README.md
│
└── jf-cli-plugin/       JFrog CLI plugin (Go) — native jf integration
    ├── commands/create.go
    ├── main.go
    ├── Makefile
    └── README.md
```

## Which Should I Use?

| | Shell Scripts (`scripts/`) | JFrog CLI Plugin (`jf-cli-plugin/`) |
|---|---|---|
| **Run with** | `./create-rb-via-cli.sh build-name` | `jf rb-manager create build-name` |
| **Dependencies** | `jf` + `jq` | `jf` only (Go at build-time) |
| **Build step** | None | `make install` once |
| **Configuration** | Environment variables | CLI flags (+ env var fallback) |
| **Interactive mode** | Prompts when a TTY is detected | Never interactive |
| **Type safety** | Manual jq/bash validation | Go compiler + regex validation |
| **AQL injection risk** | Mitigated via runtime sanitisation | Structurally prevented via input validation |
| **Testability** | Difficult | Standard Go test framework |

### Use the Shell Scripts When

- You need **zero setup** — just copy a `.sh` file and run it.
- You're doing a **one-off or ad-hoc** release bundle creation from your terminal.
- You want to **quickly iterate** on the logic (edit and run, no compile step).
- Your CI environment doesn't have Go but does have `jq`.
- You want **interactive confirmation** before proceeding when builds are missing.

### Use the JFrog CLI Plugin When

- You want a **native `jf` subcommand** that feels like a first-class tool.
- You're running in **CI/CD pipelines** where non-interactive behaviour is mandatory.
- You need **type-safe, testable code** for production reliability.
- You want to **distribute it** to a team via `jf plugin install` or a binary.
- You prefer **CLI flags** over environment variables for configuration.
- You want to **eliminate the `jq` dependency**.

## Quick Start

### Shell Scripts

```bash
cd scripts/

# Dry run — see what would be created
DRY_RUN=true ./create-rb-via-cli.sh cg-webgoat-by

# Create a release bundle
RB_NAME="prod-release" RB_VERSION="1.0.0" ./create-rb-via-cli.sh cg-webgoat-by cg-petclinic-by
```

See [`scripts/README.md`](scripts/README.md) for full documentation, environment variables, and CI mode details.

### JFrog CLI Plugin

```bash
cd jf-cli-plugin/

# Build and install
make install

# Dry run
jf rb-manager create --dry-run cg-webgoat-by

# Create a release bundle
jf rb-manager create --rb-name=prod-release --rb-version=1.0.0 cg-webgoat-by cg-petclinic-by
```

See [`jf-cli-plugin/README.md`](jf-cli-plugin/README.md) for full documentation, flags, and CI usage examples.

## Feature Parity

Both implementations provide identical core behaviour:

- **AQL-based build discovery** with environment property filtering
- **Latest-build-per-name extraction** with dual field-name support (`build.name` / `name`)
- **Input validation** — build names, RB name/version, signing key restricted to safe characters
- **AQL injection prevention** — unsafe characters rejected before query construction
- **Missing build handling** — configurable fail/warn/prompt behaviour
- **Dry-run mode** — inspect the payload without creating anything
- **Release Bundle v2 creation** via the JFrog Lifecycle REST API

## Prerequisites

- [JFrog CLI](https://docs.jfrog-applications.jfrog.io/jfrog-applications/jfrog-cli/install) configured with a server (`jf config add`)
- Admin access (required for AQL builds-domain queries)
- Shell scripts additionally require [`jq`](https://jqlang.github.io/jq/download/)
- JFrog CLI plugin additionally requires [Go 1.22+](https://go.dev/dl/) (build-time only)
