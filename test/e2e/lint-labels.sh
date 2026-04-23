#!/bin/bash
#
# Lint: Verify all e2e test specs have a read-only or mutating label.
#
# Every It() block must either:
#   1. Have Label("read-only") or Label("mutating") directly, OR
#   2. Be nested inside a Describe/Context that has the label
#
# This script checks that no top-level It() blocks are missing labels.
# Nested It() blocks inherit from their parent Describe/Context.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_FILE="$SCRIPT_DIR/configure_alertmanager_operator_tests.go"

ERRORS=0

# Find It( calls that are NOT inside a labeled Describe/Context and don't have their own label.
# Top-level It blocks start with a single tab. Nested ones have more indentation.
# We check single-tab It() calls for labels.
while IFS= read -r line; do
  lineno=$(echo "$line" | cut -d: -f1)
  content=$(echo "$line" | cut -d: -f2-)
  if ! echo "$content" | grep -q 'Label('; then
    echo "ERROR: line $lineno: It() block missing Label(\"read-only\") or Label(\"mutating\"): $content"
    ERRORS=$((ERRORS + 1))
  fi
done < <(grep -n '^	It(' "$TEST_FILE" | grep -v 'PIt(')

if [[ $ERRORS -gt 0 ]]; then
  echo ""
  echo "FAIL: $ERRORS It() block(s) missing required labels."
  echo "Every top-level It() must have Label(\"read-only\") or Label(\"mutating\")."
  echo "Tests nested inside a labeled Describe/Context inherit the parent label."
  exit 1
fi

echo "OK: All top-level It() blocks have required labels."
