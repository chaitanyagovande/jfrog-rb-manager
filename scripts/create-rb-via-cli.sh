#!/bin/bash
###############################################################################
# Create Release Bundle v2 via JFrog CLI (jf rbc)
#
# Finds the latest build (by created date) for each supplied build name
# where the build has a specific environment variable, then creates a
# Release Bundle v2 using the JFrog CLI release-bundle-create command
# with a generated spec file.
#
# Prerequisites:
#   - JFrog CLI configured with a server (jf config add)
#   - Admin access (required for AQL builds domain queries)
#   - jq installed
#
# Usage:
#   ./create-rb-via-cli.sh <build-name-1> [build-name-2] [build-name-3] ...
#
# Environment variables (override defaults):
#   ENV_KEY       - Build property key to filter on
#                   (default: buildInfo.env.GITHUB_REPOSITORY_OWNER)
#   ENV_VALUE     - Build property value to match
#                   (default: chaitanyagovande)
#   RB_NAME       - Release Bundle name (default: my-release-bundle)
#   RB_VERSION    - Release Bundle version (default: 1.0.0)
#   SIGNING_KEY   - GPG signing key name (default: empty, uses platform default)
#   INCLUDE_DEPS  - Include build dependencies in RB (default: false)
#   DRY_RUN       - If "true", generate spec but don't create RB (default: false)
#   SYNC          - Wait for RB creation to complete (default: true)
#   PROJECT_KEY   - JFrog project key, if using projects (default: empty)
#   FAIL_ON_MISSING - In non-interactive mode: "true" to fail when builds are
#                     missing, "false" to warn and continue (default: true)
#
# Non-interactive / CI mode:
#   Automatically enabled when CI=true (set by most CI systems) or when
#   stdin is not a TTY. Disables all interactive prompts.
###############################################################################

set -euo pipefail

# ─── Utility functions ──────────────────────────────────────────────────────

log()  { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
die()  { log "ERROR: $*" >&2; exit 1; }

validate_pattern() {
    local value="$1" label="$2" pattern="$3"
    if [[ ! "$value" =~ $pattern ]]; then
        die "${label} contains invalid characters: '${value}' (expected pattern: ${pattern})"
    fi
}

validate_boolean() {
    local value="$1" label="$2"
    if [[ "$value" != "true" && "$value" != "false" ]]; then
        die "${label} must be 'true' or 'false', got: '${value}'"
    fi
}

sanitize_aql_value() {
    local value="$1" label="$2"
    if [[ "$value" =~ [\"\\$\`\'] ]]; then
        die "${label} contains characters unsafe for AQL interpolation: '${value}'"
    fi
}

validate_json_response() {
    local response="$1" context="$2"
    if ! printf '%s' "$response" | jq empty 2>/dev/null; then
        die "${context}: response is not valid JSON. First 200 chars: ${response:0:200}"
    fi
    if printf '%s' "$response" | jq -e '.errors' >/dev/null 2>&1; then
        log "ERROR: ${context}:"
        printf '%s' "$response" | jq '.errors' >&2
        exit 1
    fi
}

# ─── Defaults ────────────────────────────────────────────────────────────────
ENV_KEY="${ENV_KEY:-buildInfo.env.GITHUB_REPOSITORY_OWNER}"
ENV_VALUE="${ENV_VALUE:-chaitanyagovande}"
RB_NAME="${RB_NAME:-my-release-bundle}"
RB_VERSION="${RB_VERSION:-1.0.0}"
SIGNING_KEY="${SIGNING_KEY:-}"
INCLUDE_DEPS="${INCLUDE_DEPS:-false}"
DRY_RUN="${DRY_RUN:-false}"
FAIL_ON_MISSING="${FAIL_ON_MISSING:-true}"
SYNC="${SYNC:-true}"

if [[ "${CI:-false}" == "true" ]] || [[ ! -t 0 ]]; then
    INTERACTIVE=false
else
    INTERACTIVE=true
fi
PROJECT_KEY="${PROJECT_KEY:-}"

# ─── Validate inputs ────────────────────────────────────────────────────────
if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <build-name-1> [build-name-2] ..."
    echo ""
    echo "Examples:"
    echo "  $0 cg-webgoat-by"
    echo "  $0 cg-webgoat-by cg-petclinic-by cg-juice-shop-by"
    echo "  RB_NAME=prod-bundle RB_VERSION=2.1.0 $0 cg-webgoat-by cg-petclinic-by"
    exit 1
fi

command -v jq >/dev/null 2>&1 || die "jq is required but not installed."
command -v jf >/dev/null 2>&1 || die "JFrog CLI (jf) is required but not installed."

readonly SAFE_NAME_PATTERN='^[a-zA-Z0-9._-]+$'

for NAME in "$@"; do
    validate_pattern "$NAME" "Build name" "$SAFE_NAME_PATTERN"
    sanitize_aql_value "$NAME" "Build name '${NAME}'"
done

sanitize_aql_value "$ENV_KEY" "ENV_KEY"
sanitize_aql_value "$ENV_VALUE" "ENV_VALUE"
validate_pattern "$RB_NAME" "RB_NAME" "$SAFE_NAME_PATTERN"
validate_pattern "$RB_VERSION" "RB_VERSION" "$SAFE_NAME_PATTERN"
validate_boolean "$INCLUDE_DEPS" "INCLUDE_DEPS"
validate_boolean "$DRY_RUN" "DRY_RUN"
validate_boolean "$FAIL_ON_MISSING" "FAIL_ON_MISSING"
validate_boolean "$SYNC" "SYNC"
if [[ -n "$SIGNING_KEY" ]]; then
    validate_pattern "$SIGNING_KEY" "SIGNING_KEY" "$SAFE_NAME_PATTERN"
fi
if [[ -n "$PROJECT_KEY" ]]; then
    validate_pattern "$PROJECT_KEY" "PROJECT_KEY" "$SAFE_NAME_PATTERN"
fi

BUILD_NAMES=("$@")

echo "============================================================"
echo " Release Bundle v2 via JFrog CLI (jf rbc)"
echo "============================================================"
echo " Environment filter : ${ENV_KEY} = ${ENV_VALUE}"
echo " Build names        : ${BUILD_NAMES[*]}"
echo " Release Bundle     : ${RB_NAME} v${RB_VERSION}"
echo " Signing key        : ${SIGNING_KEY:-<platform default>}"
echo " Include deps       : ${INCLUDE_DEPS}"
echo " Sync               : ${SYNC}"
echo " Project key        : ${PROJECT_KEY:-<none>}"
echo " Dry run            : ${DRY_RUN}"
echo " Interactive        : ${INTERACTIVE}"
echo "============================================================"
echo ""

# ─── Step 1: Construct and run the AQL query ────────────────────────────────
OR_ITEMS=""
for NAME in "${BUILD_NAMES[@]}"; do
    OR_ITEMS="${OR_ITEMS},{\"name\": \"${NAME}\"}"
done
OR_ITEMS="${OR_ITEMS:1}"  # trim leading comma

AQL_QUERY="builds.find({\"\$and\": [{\"property.key\": \"${ENV_KEY}\"}, {\"property.value\": \"${ENV_VALUE}\"}, {\"\$or\": [${OR_ITEMS}]}]}).include(\"name\",\"number\",\"created\").sort({\"\$desc\": [\"created\"]}).limit(100000)"

log "[1/4] Running AQL query..."
AQL_RESULT=$(jf rt curl -s -X POST \
    -H "Content-Type: text/plain" \
    -d "$AQL_QUERY" \
    /api/search/aql)

validate_json_response "$AQL_RESULT" "AQL query"

TOTAL=$(printf '%s' "$AQL_RESULT" | jq '.range.total')
if [[ -z "$TOTAL" || "$TOTAL" == "null" ]]; then
    die "AQL response missing .range.total — unexpected response format."
fi
log "Found ${TOTAL} total matching build(s)."

if [[ "$TOTAL" -eq 0 ]]; then
    die "No builds found matching the criteria. Nothing to bundle."
fi

# ─── Step 2: Extract latest build number per build name ─────────────────────
echo ""
log "[2/4] Extracting latest build per name..."

LATEST_BUILDS=$(printf '%s' "$AQL_RESULT" | jq '
    [.results | map({
        name:    (."build.name"    // .name),
        number:  (."build.number"  // .number  | tostring),
        created: (."build.created" // .created)
    }) | group_by(.name)[] | sort_by(.created) | reverse | .[0]]
    | map({build_name: .name, build_number: .number, created: .created})
')

if printf '%s' "$LATEST_BUILDS" | jq -e 'length == 0 or (.[0].build_name == null)' >/dev/null 2>&1; then
    log "DEBUG: first AQL result object:"
    printf '%s' "$AQL_RESULT" | jq '.results[0]' >&2
    die "Failed to extract build info — AQL response has unexpected field names."
fi

printf '%s' "$LATEST_BUILDS" | jq -r '.[] | "       \(.build_name) #\(.build_number) (created: \(.created))"'

FOUND_NAMES=$(printf '%s' "$LATEST_BUILDS" | jq -r '.[].build_name')
MISSING=()
for NAME in "${BUILD_NAMES[@]}"; do
    if ! printf '%s\n' "$FOUND_NAMES" | grep -qx "$NAME"; then
        MISSING+=("$NAME")
    fi
done

if [[ ${#MISSING[@]} -gt 0 ]]; then
    echo ""
    log "WARNING: The following build name(s) were NOT found:"
    for M in "${MISSING[@]}"; do
        echo "         - $M"
    done

    if [[ "$INTERACTIVE" == "true" ]]; then
        echo ""
        read -r -p "Continue without them? (y/N): " CONFIRM
        if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
            echo "Aborted."
            exit 1
        fi
    elif [[ "$FAIL_ON_MISSING" == "true" ]]; then
        die "Missing builds in non-interactive mode (set FAIL_ON_MISSING=false to continue anyway)."
    else
        log "Continuing without missing builds (FAIL_ON_MISSING=false)."
    fi
fi

# ─── Step 3: Generate the JFrog CLI spec file ───────────────────────────────
echo ""
log "[3/4] Generating spec file..."

SPEC_FILE=$(mktemp /tmp/rb-spec-XXXXXX.json)
trap 'rm -f "$SPEC_FILE"' EXIT

printf '%s' "$LATEST_BUILDS" | jq --argjson deps "$INCLUDE_DEPS" '{
    files: [.[] | {
        build: (.build_name + "/" + .build_number),
        includeDeps: ($deps | tostring)
    }]
}' > "$SPEC_FILE"

log "Spec file: ${SPEC_FILE}"
echo ""
jq . "$SPEC_FILE"

# ─── Step 4: Create the Release Bundle via jf rbc ───────────────────────────
echo ""
if [[ "$DRY_RUN" == "true" ]]; then
    log "[4/4] DRY RUN — skipping Release Bundle creation."
    echo "       Spec file above is what would be used."
    echo ""
    echo "       To create manually, run:"
    echo "       jf rbc --spec=\"${SPEC_FILE}\" \"${RB_NAME}\" \"${RB_VERSION}\""
    exit 0
fi

log "[4/4] Creating Release Bundle v2 via jf rbc..."

CMD=(jf rbc --spec="$SPEC_FILE")

if [[ -n "$SIGNING_KEY" ]]; then
    CMD+=(--signing-key="$SIGNING_KEY")
fi

if [[ "$SYNC" == "true" ]]; then
    CMD+=(--sync=true)
fi

if [[ -n "$PROJECT_KEY" ]]; then
    CMD+=(--project="$PROJECT_KEY")
fi

CMD+=("$RB_NAME" "$RB_VERSION")

log "Running: ${CMD[*]}"
echo ""

"${CMD[@]}"

echo ""
echo "============================================================"
echo " Release Bundle created: ${RB_NAME} v${RB_VERSION}"
echo "============================================================"
