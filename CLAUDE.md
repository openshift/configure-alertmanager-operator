# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Configure Alertmanager Operator is a Kubernetes operator for OpenShift Dedicated that dynamically manages Alertmanager configurations based on the presence of secrets and configmaps. It watches for changes to specific resources in the `openshift-monitoring` namespace and updates Alertmanager routing accordingly.

## Key Architecture Components

### Controllers
- **Secret Controller** (`controllers/secret_controller.go`): The main controller that watches secrets and configmaps in `openshift-monitoring` namespace and reconciles Alertmanager configuration
- Watches: `alertmanager-main`, `goalert-secret`, `pd-secret`, `dms-secret` secrets and `ocm-agent`, `managed-namespaces`, `ocp-namespaces` configmaps

### Core Packages
- **config/** - Operator configuration and settings
- **pkg/types/** - Alertmanager configuration types (imported from Prometheus Alertmanager but pared down)
- **pkg/metrics/** - Prometheus metrics exposure and service creation
- **pkg/readiness/** - Cluster readiness checks (waits for `osd-cluster-ready` job completion)

### Alert Routing Types
The operator supports multiple alert receivers:
- **GoAlert** - Routes alerts to GoAlert service (high/low priority + heartbeat)
- **PagerDuty** - Routes alerts to PagerDuty service
- **Dead Man's Snitch** - Healthcheck/heartbeat monitoring
- **OCM Agent** - OpenShift Cluster Manager integration

## Build Commands

The project uses a boilerplate-based build system with standard Go operator targets:

```bash
# Build the operator binary
make go-build

# Run unit tests
make test
make go-test

# Lint code
make lint
make go-check

# Build container image
make docker-build

# Push container image
make docker-push

# Run tests in container
make container-test
```

## Development Commands

```bash
# Build and push custom image (override repository)
make IMAGE_REPOSITORY=my-user docker-build docker-push

# Run specific test packages
make go-test TESTTARGETS="./controllers/..."

# Set up test environment
make setup-envtest

# Generate code/manifests (if applicable)
make generate
```

## Testing

### Unit Tests
- Located in `*_test.go` files alongside source code
- Uses Ginkgo/Gomega testing framework
- Controller tests in `controllers/secret_controller_test.go`

### E2E Tests
- Located in `test/e2e/` directory
- Uses sigs.k8s.io/e2e-framework
- Tests operator behavior in live cluster environment

### Test Environment Setup
- Uses envtest for unit tests (simulated Kubernetes API)
- ENVTEST_K8S_VERSION=1.23 for Kubernetes version compatibility
- Test targets automatically exclude vendor and e2e directories

## Important Configuration

### Environment Variables
- `WATCH_NAMESPACE` - Namespace to watch (typically `openshift-monitoring`)
- `POD_NAME` - Name of the operator pod
- `OPERATOR_NAME` - Name of the operator
- `MAX_CLUSTER_AGE_MINUTES` - Maximum cluster age for readiness checks

### Key Namespaces
- `openshift-monitoring` - Primary namespace for all watched resources
- `openshift-cluster-version` - For cluster version operator scaling
- `openshift-operator-lifecycle-manager` - For OLM operator management

## Secret Controller Matching Rules

When modifying matching rules in the secret controller, be aware that rules are processed in order. Earlier matching rules that capture alerts will prevent subsequent rules from being evaluated. The OCM agent matching rule is an example where matched alerts skip remaining rules.

## Development Notes

- The operator uses FIPS-enabled builds (`FIPS_ENABLED=true`)
- Built with Go 1.23.0/toolchain 1.24.1
- Uses controller-runtime framework for Kubernetes interactions
- Prometheus metrics are exposed via ServiceMonitor for cluster monitoring
- OLM (Operator Lifecycle Manager) manages operator deployment in production clusters