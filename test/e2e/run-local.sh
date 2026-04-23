#!/bin/bash
#
# Run CAMO e2e tests locally against a running cluster.
#
# Usage:
#   ./test/e2e/run-local.sh                    # read-only tests (safe for any cluster)
#   ./test/e2e/run-local.sh --all              # all tests including mutating (use on test clusters only)
#   ./test/e2e/run-local.sh --focus "roles"    # run tests matching a regex
#
# Prerequisites:
#   - Logged into a cluster: oc whoami
#   - KUBECONFIG set or oc login completed
#   - For backplane clusters: run 'ocm backplane elevate "reason"' first
#
# The script builds the e2e binary if needed, then runs with appropriate filters.
# On backplane clusters, it automatically sets E2E_IMPERSONATE_USER so the Go
# test binary can impersonate backplane-cluster-admin for operations that require
# elevated RBAC (e.g., Prometheus SA token creation).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BINARY="$REPO_ROOT/e2e.test"

MODE="read-only"
FOCUS=""
EXTRA_ARGS=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --all)
      MODE="all"
      shift
      ;;
    --focus)
      FOCUS="$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: $0 [--all] [--focus REGEX] [-- EXTRA_GINKGO_ARGS...]"
      echo ""
      echo "Modes:"
      echo "  (default)   Run read-only tests only (safe for production clusters)"
      echo "  --all       Run all tests including mutating (test clusters only)"
      echo "  --focus RE  Run tests matching regex"
      echo ""
      echo "Backplane clusters:"
      echo "  Run 'ocm backplane elevate \"reason\"' before this script."
      echo "  The script auto-detects backplane and sets impersonation."
      echo ""
      echo "Examples:"
      echo "  $0                              # read-only validation"
      echo "  $0 --focus 'roles exist'        # specific test"
      echo "  $0 --all                        # full suite (test cluster only)"
      exit 0
      ;;
    --)
      shift
      EXTRA_ARGS=("$@")
      break
      ;;
    *)
      EXTRA_ARGS+=("$1")
      shift
      ;;
  esac
done

echo "=== CAMO E2E Local Test Runner ==="

# Check cluster access
if ! oc whoami &>/dev/null; then
  echo "ERROR: Not logged into a cluster. Run 'oc login' or set KUBECONFIG first."
  exit 1
fi

CLUSTER=$(oc whoami --show-server 2>/dev/null || echo "unknown")
USER=$(oc whoami 2>/dev/null || echo "unknown")
echo "Cluster: $CLUSTER"
echo "User:    $USER"
echo "Mode:    $MODE"

# Detect backplane and set impersonation if elevated
if [[ "$CLUSTER" == *"backplane"* ]] && [[ -z "${E2E_IMPERSONATE_USER:-}" ]]; then
  if oc config view --minify -o jsonpath='{.contexts[0].context.extensions}' 2>/dev/null | grep -q "ElevateContext"; then
    export E2E_IMPERSONATE_USER="backplane-cluster-admin"
    echo "Backplane: elevated, impersonating $E2E_IMPERSONATE_USER"
  else
    echo ""
    echo "WARNING: Backplane cluster detected but no elevation found."
    echo "Some tests may fail due to restricted RBAC."
    echo "Run 'ocm backplane elevate \"reason\"' first for full test coverage."
  fi
fi

echo ""

# Build if binary doesn't exist or source is newer
if [[ ! -f "$BINARY" ]] || [[ "$SCRIPT_DIR/configure_alertmanager_operator_tests.go" -nt "$BINARY" ]]; then
  echo "Building e2e binary..."
  cd "$REPO_ROOT"
  go test ./test/e2e -v -c --tags=osde2e -o "$BINARY"
  echo ""
fi

# Construct ginkgo args
GINKGO_ARGS=(--ginkgo.v)

if [[ -n "$FOCUS" ]]; then
  GINKGO_ARGS+=(--ginkgo.focus "$FOCUS")
elif [[ "$MODE" == "read-only" ]]; then
  GINKGO_ARGS+=(--ginkgo.label-filter "read-only")
fi

if [[ ${#EXTRA_ARGS[@]} -gt 0 ]]; then
  GINKGO_ARGS+=("${EXTRA_ARGS[@]}")
fi

echo "Running: $BINARY ${GINKGO_ARGS[*]}"
echo "---"

export DISABLE_JUNIT_REPORT=true
if [[ "$MODE" == "read-only" ]]; then
  export E2E_READ_ONLY=true
fi

"$BINARY" "${GINKGO_ARGS[@]}"
