# configure-alertmanager-operator

[![Go Report Card](https://goreportcard.com/badge/github.com/openshift/configure-alertmanager-operator)](https://goreportcard.com/report/github.com/openshift/configure-alertmanager-operator)
[![GoDoc](https://godoc.org/github.com/openshift/configure-alertmanager-operator?status.svg)](https://godoc.org/github.com/openshift/configure-alertmanager-operator)
[![codecov](https://codecov.io/gh/openshift/configure-alertmanager-operator/branch/master/graph/badge.svg)](https://codecov.io/gh/openshift/configure-alertmanager-operator)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

- [configure-alertmanager-operator](#configure-alertmanager-operator)
  - [Summary](#summary)
  - [Cluster Readiness](#cluster-readiness)
  - [Metrics](#metrics)
  - [Alerts](#alerts)
  - [Testing](#testing)
    - [Building](#building)
    - [Deploying](#deploying)
      - [Prevent Overwrites](#prevent-overwrites)
      - [Replace the Image](#replace-the-image)

## Summary
The Configure Alertmanager Operator was created for the OpenShift Dedicated platform to dynamically manage Alertmanager configurations based on the presence or absence of secrets containing a GoAlert URLs, Pager Duty RoutingKey, and [Dead Man's Snitch](https://deadmanssnitch.com) URL. When the secret is created/updated/deleted, the associated Receiver and Route will be created/updated/deleted within the Alertmanager config.

The operator contains the following components:

* Secret controller: watches the `openshift-monitoring` namespace for any changes to relevant Secrets or ConfigMaps that are used in the configuration of Alertmanager. For more information on this see [Secret Controller](#secret-controller) below.

* Types library: these types are imported from the Alertmanager [Config](https://github.com/prometheus/alertmanager/blob/master/config/config.go) library and pared down to suit our config needs. (Since their library is [intended for internal use only](https://github.com/prometheus/alertmanager/pull/1804#issuecomment-482038079)).

## Secret Controller

The Secret Controller watches over the resources in the table below. Changes to these resources will prompt the controller to reconcile.

**Note**: When making changes to [matching rules](https://github.com/openshift/configure-alertmanager-operator/blob/master/controllers/secret_controller.go) in secret controller, check to make sure the newly added rules won't get ignored. e.g. when a matching rule above the new rule captures it and don't continue to match the following rules, the new matching rule will be ignored. [ocm-agent matching](https://github.com/openshift/configure-alertmanager-operator/blob/master/controllers/secret_controller.go#L504) rule is an example, if an alert matches this rule, the rules after will never gets ignored.

| Resource Type | Resource Namespace/Name                   | Reason for watching                                                                                                                                    |
|---------------|-------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| Secret        | `openshift-monitoring/alertmanager-main`  | Represents the Alertmanager Configuration that the operator creates/maintains the state of.                                                            |
| Secret        | `openshift-monitoring/goalert-secret`     | Indicates that the operator should configure GoAlert routing. Contains 3 values used by GoAlert; URL for high alerts, low alerts, and a heartbeat.              |
| Secret        | `openshift-monitoring/pd-secret`          | Indicates that the operator should configure PagerDuty routing. Contains the PagerDuty API Key that is used for PagerDuty communications.              |
| Secret        | `openshift-monitoring/dms-secret`         | Indicates that the operator should configure DeadmansSnitch routing. Contains the DeadmansSnitch URL that the Alertmanager should report readiness to. |
| ConfigMap     | `openshift-monitoring/ocm-agent`          | Indicates that the operator should configure OCM Agent routing. Contains the OCM Agent service URL that Alertmanager should route alerts to.           |
| ConfigMap     | `openshift-monitoring/managed-namespaces` | Defines a list of OpenShift "managed" namespaces. The operator will route alerts originating from these namespaces to PagerDuty and/or GoAlert.                       |
| ConfigMap     | `openshift-monitoring/ocp-namespaces`     | Defines a list of OpenShift Container Platform namespaces. The operator will route alerts originating from these namespaces to PagerDuty and/or GoAlert.              |

## Alertmanager Config Validation

The operator validates all Alertmanager configurations before writing them to the `alertmanager-main` secret. This prevents invalid configurations from being deployed, which could cause Alertmanager to fail on restart.

### How Validation Works

1. **Pre-write Validation**: Before writing any configuration to `alertmanager-main`, the operator validates it using Prometheus Alertmanager's official `config.Load()` function - the exact same validation that Alertmanager performs on startup.

2. **Validation Failure Handling**: If validation fails:
   - The invalid config is **not written** to the secret (preserving the last-known-good configuration)
   - A Kubernetes Event is created in `openshift-monitoring` namespace with reason `AlertmanagerConfigValidationFailure`
   - The `alertmanager_config_validation_failed` metric is set to `1` (failed)
   - The reconcile loop returns an error, triggering automatic retry

3. **Validation Success**: If validation succeeds:
   - The config is written to `alertmanager-main`
   - The `alertmanager_config_validation_failed` metric is set to `0` (succeeded)

### Monitoring Validation Status

**Via Prometheus Metric**:
```promql
alertmanager_config_validation_failed{name="configure-alertmanager-operator"}
```
- Value `0` = validation succeeded (config is valid)
- Value `1` = validation failed (config is invalid)

**Via Kubernetes Events**:
```bash
oc get events -n openshift-monitoring --field-selector reason=AlertmanagerConfigValidationFailure
```

Failed validation events include:
- The specific validation error from Alertmanager
- Guidance to check source secrets and configmaps for invalid data
- A reference to operator logs for detailed debugging

### Common Validation Failures

- **Invalid label names**: Prometheus label names must match `[a-zA-Z_][a-zA-Z0-9_]*` (no hyphens allowed)
- **Duplicate receiver names**: Each receiver must have a unique name
- **Missing required fields**: Route and at least one receiver are required
- **Invalid duration formats**: Must use valid Go duration strings (e.g., "5m", "1h")
- **Invalid regex patterns**: MatchRE patterns must be valid regular expressions

## Cluster Readiness
To avoid alert noise while a cluster is in the early stages of being installed and configured, this operator waits to configure Pager Duty -- effectively silencing alerts -- until a predetermined set of health checks, performed by [osd-cluster-ready](https://github.com/openshift/osd-cluster-ready/), has completed.

This determination is made through the presence of a completed `Job` named `osd-cluster-ready` in the `openshift-monitoring` namespace.

## Metrics
The Configure Alertmanager Operator exposes the following Prometheus metrics:

| Metric name                                    | Purpose                                                                                               |
|------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| `ga_secret_exists`                             | indicates that a Secret named `goalert-secret` exists in the `openshift-monitoring` namespace.        |
| `pd_secret_exists`                             | indicates that a Secret named `pd-secret` exists in the `openshift-monitoring` namespace.             |
| `dms_secret_exists`                            | indicates that a Secret named `dms-secret` exists in the `openshift-monitoring` namespace.            |
| `am_secret_exists`                             | indicates that a Secret named `alertmanager-main` exists in the `openshift-monitoring` namespace.     |
| `managed_namespaces_configmap_exists`          | indicates that a ConfigMap named `managed-namespaces` exists in the `openshift-monitoring` namespace. |
| `ocp_namespaces_configmap_exists`              | indicates that a ConfigMap named `ocp-namespaces` exists in the `openshift-monitoring` namespace.     |
| `am_secret_contains_ga`                        | indicates the GoAlert receiver is present in alertmanager.yaml.                                       |
| `am_secret_contains_pd`                        | indicates the Pager Duty receiver is present in alertmanager.yaml.                                    |
| `am_secret_contains_dms`                       | indicates the Dead Man's Snitch receiver is present in alertmanager.yaml.                             |
| `alertmanager_config_validation_failed`        | indicates Alertmanager config validation failed: `1` = failed, `0` = succeeded.                       |

The operator creates a `Service` and `ServiceMonitor` named `configure-alertmanager-operator` to expose these metrics to Prometheus.

## Alerts
The following alerts are added to Prometheus as part of configure-alertmanager-operator:
* Mismatch between DMS secret and DMS Alertmanager config.
* Mismatch between GoAlert secret and GoAlert Alertmanager config.
* Mismatch between PD secret and PD Alertmanager config.
* Alertmanager config secret does not exist.

## Testing
Tips for testing on a personal cluster:

### Building
You may build (`make docker-build`) and push (`make docker-push`) the operator image to a personal repository by overriding components of the image URI:
- `IMAGE_REGISTRY` overrides the *registry* (default: `quay.io`)
- `IMAGE_REPOSITORY` overrides the *organization* (default: `app-sre`)
- `IMAGE_NAME` overrides the *repository name* (default: `managed-cluster-validating-webhooks`)
- `OPERATOR_IMAGE_TAG` overrides the *image tag*. (By default this is generated based on the current commit of your local clone of the git repository; but `make docker-build` will also always tag `latest`)

For example, to build, tag, and push `quay.io/my-user/configure-alertmanager-operator:latest`, you can run:

```
make IMAGE_REPOSITORY=my-user docker-build docker-push
```

### Deploying

#### Prevent Overwrites

Note: This step requires elevated permissions

This operator is managed by OLM, so you must switch that off, or your changes to the operator's Deployment will be overwritten:

```
oc scale deploy/cluster-version-operator --replicas=0 -n openshift-cluster-version
oc scale deploy/olm-operator --replicas=0 -n openshift-operator-lifecycle-manager
```

**NOTE: Don't forget to revert these changes when you have finished testing:**

```
oc scale deploy/olm-operator --replicas=1 -n openshift-operator-lifecycle-manager
oc scale deploy/cluster-version-operator --replicas=1 -n openshift-cluster-version
```

#### Replace the Image
Edit the operator's deployment (`oc set image deployment configure-alertmanager-operator -n openshift-monitoring *=<IMAGE>`), replacing the `image:` with the URI of the image you built [above](#building). The deployment will automatically delete and replace the running pod.

**NOTE:** If you are testing coordination with the osd-cluster-ready job, you may need to set the `MAX_CLUSTER_AGE_MINUTES` environment variable in the deployment's `configure-alertmanager-operator` container definition.
For example, to ensure the osd-cluster-ready Job is checked in a cluster less than 1048576 minutes (~two years) old:

```yaml
        containers:
        - command:
          - configure-alertmanager-operator
          env:
          - name: WATCH_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
          - name: POD_NAME
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.name
          - name: OPERATOR_NAME
            value: configure-alertmanager-operator
          ### Add this entry ###
          - name: MAX_CLUSTER_AGE_MINUTES
            value: "1048576"
          image: quay.io/2uasimojo/configure-alertmanager-operator:latest
          imagePullPolicy: Always
          name: configure-alertmanager-operator
          resources: {}
          terminationMessagePath: /dev/termination-log
          terminationMessagePolicy: File
```
