# Testing Local Operator Against a Remote Cluster

This guide explains how to run the operator locally on your laptop while connecting to a remote OpenShift cluster for testing and development purposes.

## Use Cases

- **Memory profiling**: Measure operator memory usage with different cache configurations
- **Development testing**: Test code changes without rebuilding container images
- **Cache behavior validation**: Verify namespace scoping works correctly
- **Performance analysis**: Profile CPU and memory usage under real cluster load

## Prerequisites

- `oc` CLI tool installed and authenticated to target cluster
- `go` 1.21+ installed locally
- Admin access to create ServiceAccounts and RBAC in the cluster
- `jq` for JSON parsing (optional, for token extraction)

## Overview

Running the operator locally requires:

1. **Read-only ServiceAccount**: Prevents accidental cluster modifications during testing
2. **SKIP_LEADER_ELECTION**: Bypasses leader election lock (which requires write permissions)
3. **Custom kubeconfig**: Uses ServiceAccount token instead of your admin credentials
4. **Local binary**: Built from your working directory with code changes

## Setup Instructions

### Step 1: Create Read-Only ServiceAccount and RBAC

Create a ServiceAccount with read-only permissions on the resources the operator needs:

```bash
# Create ServiceAccount
oc create serviceaccount configure-alertmanager-operator-readonly -n openshift-monitoring

# Create ClusterRole with read-only permissions
oc create clusterrole configure-alertmanager-operator-readonly \
  --verb=get,list,watch \
  --resource=clusterversions,proxies,infrastructures,secrets,configmaps

# Bind ClusterRole to ServiceAccount
oc create clusterrolebinding configure-alertmanager-operator-readonly \
  --clusterrole=configure-alertmanager-operator-readonly \
  --serviceaccount=openshift-monitoring:configure-alertmanager-operator-readonly
```

**What this does:**
- ServiceAccount has NO write permissions (no `create`, `update`, `delete` verbs)
- Operator can read cluster-scoped resources (ClusterVersion, Proxy, Infrastructure)
- Operator can read Secrets and ConfigMaps cluster-wide (needed for cache initialization)
- Prevents accidental modifications to `alertmanager-main` secret or other cluster resources

### Step 2: Generate ServiceAccount Token and Create Kubeconfig

Generate a short-lived token for the ServiceAccount and create a custom kubeconfig:

```bash
# Generate 1-hour token
TOKEN=$(oc create token configure-alertmanager-operator-readonly -n openshift-monitoring --duration=1h)

# Get cluster API URL
CLUSTER_API=$(oc config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# Create read-only kubeconfig
cat > /tmp/readonly-kubeconfig.yaml <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: ${CLUSTER_API}
  name: readonly-cluster
contexts:
- context:
    cluster: readonly-cluster
    namespace: openshift-monitoring
    user: readonly-user
  name: readonly-context
current-context: readonly-context
users:
- name: readonly-user
  user:
    token: ${TOKEN}
EOF

# Verify kubeconfig works (should list secrets without errors)
KUBECONFIG=/tmp/readonly-kubeconfig.yaml oc get secrets -n openshift-monitoring
```

**Notes:**
- Token expires after 1 hour - regenerate if needed
- `insecure-skip-tls-verify: true` is used for simplicity (replace with CA cert for production-like testing)
- Kubeconfig is scoped to `openshift-monitoring` namespace

### Step 3: Build and Run Operator Locally

Build the operator binary from your current working directory and run it:

```bash
# Build operator binary
make build

# Run operator locally with read-only kubeconfig
KUBECONFIG=/tmp/readonly-kubeconfig.yaml \
SKIP_LEADER_ELECTION=true \
./configure-alertmanager-operator --leader-elect=false
```

**Expected behavior:**
- Operator starts and connects to remote cluster
- Watches Secrets/ConfigMaps in `openshift-monitoring` namespace
- Attempts to reconcile when resources change
- **Write operations fail** with "Forbidden" errors (this is expected and safe)

**Example output:**
```
INFO    setup   Skipping leader election (SKIP_LEADER_ELECTION=true)
INFO    controller-runtime.metrics  Metrics server is starting to listen
INFO    controller-runtime.builder  Starting EventSource
INFO    controller-runtime.manager  Starting workers
```

You may see errors like:
```
ERROR   Failed to write alertmanager config   error="secrets \"alertmanager-main\" is forbidden:
User \"system:serviceaccount:openshift-monitoring:configure-alertmanager-operator-readonly\"
cannot update resource \"secrets\" in API group \"\" in the namespace \"openshift-monitoring\""
```

**This is expected** - the operator attempts writes but RBAC denies them. For read-only testing (memory profiling, cache validation), this is the correct behavior.

### Step 4: Monitor Operator Behavior

While the operator is running, you can monitor its behavior:

```bash
# Watch operator memory usage (run in another terminal)
while true; do
  ps aux | grep configure-alertmanager-operator | grep -v grep | \
    awk '{printf "RSS: %d MB, CPU: %.1f%%\n", $6/1024, $3}'
  sleep 5
done

# Trigger reconciliation by updating a secret
oc annotate secret pd-secret -n openshift-monitoring test-reconcile=$(date +%s)

# View operator logs (if running in background)
# Check the terminal where operator is running
```

### Step 5: Cleanup When Done

Remove all test resources from the cluster:

```bash
# Stop operator (Ctrl+C in terminal where it's running)

# Delete RBAC resources
oc delete clusterrolebinding configure-alertmanager-operator-readonly
oc delete clusterrole configure-alertmanager-operator-readonly

# Delete ServiceAccount
oc delete serviceaccount configure-alertmanager-operator-readonly -n openshift-monitoring

# Remove local kubeconfig
rm /tmp/readonly-kubeconfig.yaml

# Remove operator binary (optional)
rm ./configure-alertmanager-operator
```

## Understanding SKIP_LEADER_ELECTION

**What it does:**
- Bypasses the `leader.Become()` call in `main.go` (line 143)
- Allows operator to start without acquiring a leader election lock
- Does NOT change operator behavior beyond skipping the lock

**Why it's needed for local testing:**
- Leader election requires creating a ConfigMap lock in the cluster
- Read-only ServiceAccount cannot create ConfigMaps
- Without `SKIP_LEADER_ELECTION=true`, operator would fail immediately at startup

**What it does NOT do:**
- Does NOT make the operator read-only (operator still attempts writes)
- Does NOT disable reconciliation (operator still watches and reconciles resources)
- Does NOT change RBAC permissions (read-only permissions are from ServiceAccount)

**Production usage:**
- ⚠️ **NEVER** set `SKIP_LEADER_ELECTION=true` in production deployments
- Production MUST use leader election to prevent multiple instances from conflicting
- Only use for local development/testing with read-only credentials

## Troubleshooting

### Token has expired
**Error:** `Unable to connect to the server: failed to refresh token`

**Solution:** Regenerate the token (valid for 1 hour):
```bash
TOKEN=$(oc create token configure-alertmanager-operator-readonly -n openshift-monitoring --duration=1h)
# Recreate kubeconfig with new token (Step 2)
```

### ServiceAccount doesn't exist
**Error:** `serviceaccounts "configure-alertmanager-operator-readonly" not found`

**Solution:** Verify ServiceAccount creation:
```bash
oc get sa configure-alertmanager-operator-readonly -n openshift-monitoring
# If missing, re-run Step 1
```

### Permission denied errors in operator logs
**Error:** `cannot update resource "secrets" ... is forbidden`

**Expected behavior** - This is normal for read-only testing. The operator attempts writes but RBAC correctly denies them.

**If you need write permissions** (not recommended for testing), add write verbs to ClusterRole:
```bash
# WARNING: Only for development clusters, NOT production testing
oc create clusterrole configure-alertmanager-operator-readwrite \
  --verb=get,list,watch,create,update,patch \
  --resource=secrets,configmaps
```

### Operator crashes immediately
**Check:**
1. Verify kubeconfig is valid: `KUBECONFIG=/tmp/readonly-kubeconfig.yaml oc get ns`
2. Verify ServiceAccount has permissions: `oc auth can-i list secrets --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator-readonly`
3. Check operator logs for specific error messages

## Memory Profiling Example

To measure memory usage with namespace scoping:

```bash
# Start operator and measure memory every 5 seconds
KUBECONFIG=/tmp/readonly-kubeconfig.yaml \
SKIP_LEADER_ELECTION=true \
./configure-alertmanager-operator --leader-elect=false &

OPERATOR_PID=$!

# Monitor for 5 minutes
for i in {1..60}; do
  ps -p $OPERATOR_PID -o rss=,vsz=,pcpu= | \
    awk '{printf "%s,%d,%d,%.2f\n", strftime("%Y-%m-%d %H:%M:%S"), $1/1024, $2/1024, $3}'
  sleep 5
done > memory-usage.csv

# Stop operator
kill $OPERATOR_PID

# Analyze results
echo "Memory usage statistics (MB):"
awk -F, 'BEGIN {min=999999; max=0; sum=0; count=0}
  {
    rss=$2;
    if(rss<min) min=rss;
    if(rss>max) max=rss;
    sum+=rss;
    count++
  }
  END {
    printf "Min: %.2f MB\nMax: %.2f MB\nAvg: %.2f MB\n", min, max, sum/count
  }' memory-usage.csv
```

## Comparison Testing (Before/After Changes)

To compare memory usage between two code versions:

```bash
# Test current branch
git checkout feature-branch
make build
KUBECONFIG=/tmp/readonly-kubeconfig.yaml SKIP_LEADER_ELECTION=true \
  ./configure-alertmanager-operator --leader-elect=false &
# Monitor memory, save results, kill operator

# Test master branch
git checkout master
make build
KUBECONFIG=/tmp/readonly-kubeconfig.yaml SKIP_LEADER_ELECTION=true \
  ./configure-alertmanager-operator --leader-elect=false &
# Monitor memory, save results, kill operator

# Compare results
```

## Security Considerations

1. **Read-only by design**: ServiceAccount has no write permissions - cannot modify cluster state
2. **Token expiration**: 1-hour token lifetime limits exposure window
3. **Local kubeconfig**: Token stored in local file - delete when done
4. **No production use**: This setup is for development/testing only, never use in production
5. **Cluster selection**: Only test against development/test clusters, never production

## Related Documentation

- [SKIP_LEADER_ELECTION code comments](main.go#L132-L142)
- [Namespace scoping implementation](main.go#L97-L115)
- [E2E test permission verification](test/e2e/configure_alertmanager_operator_tests.go)
