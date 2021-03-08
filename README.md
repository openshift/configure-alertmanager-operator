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
The Configure Alertmanager Operator was created for the OpenShift Dedicated platform to dynamically manage Alertmanager configurations based on the presence or absence of secrets containing a Pager Duty RoutingKey and [Dead Man's Snitch](https://deadmanssnitch.com) URL. When the secret is created/updated/deleted, the associated Receiver and Route will be created/updated/deleted within the Alertmanager config.

The operator contains the following components:

* Secret controller: watches the `openshift-monitoring` namespace for any changes to Secrets named `alertmanager-main`, `pd-secret` or `dms-secret`.
* Types library: these types are imported from the Alertmanager [Config](https://github.com/prometheus/alertmanager/blob/master/config/config.go) library and pared down to suit our config needs. (Since their library is [intended for internal use only](https://github.com/prometheus/alertmanager/pull/1804#issuecomment-482038079)).

## Cluster Readiness
To avoid alert noise while a cluster is in the early stages of being installed and configured, this operator waits to configure Pager Duty -- effectively silencing alerts -- until a predetermined set of health checks has succeeded.
The operator uses [osd-cluster-ready](https://github.com/openshift/osd-cluster-ready/) to perform these health checks.

## Metrics
The Configure Alertmanager Operator exposes the following Prometheus metrics:

* pd_secret_exists: indicates that a Secret named `pd-secret` exists in the `openshift-monitoring` namespace.
* dms_secret_exists: indicates that a Secret named `dms-secret` exists in the `openshift-monitoring` namespace.
* am_secret_contains_pd: indicates the Pager Duty receiver is present in alertmanager.yaml.
* am_secret_contains_dms: indicates the Dead Man's Snitch receiver is present in alertmanager.yaml.

## Alerts
The following alerts are added to Prometheus as part of configure-alertmanager-operator:
* Mismatch between DMS secret and DMS Alertmanager config.
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
Edit the operator's deployment (`oc edit deployment configure-alertmanager-operator -n openshift-monitoring`), replacing the `image:` with the URI of the image you built [above](#building). The deployment will automatically delete and replace the running pod.

**NOTE:** If you are testing the osd-cluster-ready job, you may need to set the `MAX_CLUSTER_AGE_MINUTES` environment variable in the deployment's `configure-alertmanager-operator` container definition.
For example, to ensure osd-cluster-ready runs in a cluster less than 1048576 minutes (~two years) old:

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
