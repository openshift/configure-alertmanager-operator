# Configure Alertmanager Operator

## Summary
The Configure Alertmanager Operator was created for the OpenShift Dedicated platform to dynamically manage Alertmanager configurations based on the presence or absence of secrets containing a Pager Duty RoutingKey and [Dead Man's Snitch](https://deadmanssnitch.com) URL. When the secret is created/updated/deleted, the associated Receiver and Route will be created/updated/deleted within the Alertmanager config.

The operator contains the following components:

* Secret controller: watches the `openshift-monitoring` namespace for any changes to Secrets named `alertmanager-main`, `pd-secret` or `dms-secret`.
* Types library: these types are imported from the Alertmanager [Config](https://github.com/prometheus/alertmanager/blob/master/config/config.go) library and pared down to suit our config needs. (Since their library is [intended for internal use only](https://github.com/prometheus/alertmanager/pull/1804#issuecomment-482038079)).


## Metrics
The Configure Alertmanager Operator exposes the following Prometheus metrics:

* pd_secret_exists: indicates that a Secret named `pd-secret` exists in the `openshift-monitoring` namespace.
* dms_secret_exists: indicates that a Secret named `dms-secret` exists in the `openshift-monitoring` namespace.
* am_secret_contains_pd: indicates the Pager Duty receiver is present in alertmanager.yaml.
* am_secret_contains_dms: indicates the Dead Man's Snitch receiver is present in alertmanager.yaml.

## Alerts
The following alerts are added to Prometheus as part of configure-alertmanager-operator:
* Mismatch_between_DMS_secret_and_DMS_Alertmanager_config. Alerts if the Alertmanager config stays out-of-sync with Dead Man's Snitch secret for 5 minutes.
* Mismatch_between_PD_secret_and_PD_Alertmanager_config. Alerts if the Alertmanager config stays out-of-sync with the Pager Duty secret for 5 minutes.
* Alertmanager_config_secret_does_not_exist. Alerts if the `alertmanager-main` secret is missing from `openshift-monitoring` namespace for 5 minutes.
