package secret

import (
	"context"
	"fmt"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	"github.com/openshift/configure-alertmanager-operator/config"
	"github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	"github.com/openshift/configure-alertmanager-operator/pkg/readiness"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"

	configv1 "github.com/openshift/api/config/v1"
	yaml "gopkg.in/yaml.v2"
)

var log = logf.Log.WithName("secret_controller")

const (
	secretKeyPD = "PAGERDUTY_KEY"

	secretKeyDMS = "SNITCH_URL"

	cmKeyManagedNamespaces = "managed_namespaces.yaml"

	cmKeyOCPNamespaces = "managed_namespaces.yaml"

	cmKeyAddonsNamespaces = "managed_namespaces.yaml"

	secretNamePD = "pd-secret"

	secretNameDMS = "dms-secret"

	secretNameAlertmanager = "alertmanager-main"

	cmNameManagedNamespaces = "managed-namespaces"

	cmNameOCPNamespaces = "ocp-namespaces"

	cmNameAddonsNamespaces = "addons-namespaces"

	// anything routed to "null" receiver does not get routed to PD
	receiverNull = "null"

	// anything routed to "make-it-warning" receiver has severity=warning
	receiverMakeItWarning = "make-it-warning"

	// anything routed to "make-it-error" receiver has severity=error
	receiverMakeItError = "make-it-error"

	// anything routed to "make-it-critical" receiver has severity=critical
	receiverMakeItCritical = "make-it-critical"

	// anything routed to "pagerduty" will alert/notify SREP
	receiverPagerduty = "pagerduty"

	// anything going to Dead Man's Snitch (watchdog)
	receiverWatchdog = "watchdog"

	// the default receiver used by the route used for pagerduty
	defaultReceiver = receiverNull

	// global config for PagerdutyURL
	pagerdutyURL = "https://events.pagerduty.com/v2/enqueue"

	// anything routed to "ocmagent" will not alert/notify SREP and will be handled by OCM Agent
	receiverOCMAgent = "ocmagent"

	// alert label used for identifying OCM Agent-bound alerts
	managedNotificationLabel = "send_managed_notification"

	// service name for the OCM Agent service
	ocmAgentService = "ocm-agent"
	// namespace for the OCM Agent service
	ocmAgentNamespace = "openshift-ocm-agent-operator"
	// path for the OCM Agent alertmanager receiver webhook
	ocmAgentWebhookPath = "/alertmanager-receiver"

	// configmap name for OCM agent configuration
	cmNameOcmAgent = "ocm-agent"

	// OCM Agent configmap key for service URL
	cmKeyOCMAgent = "serviceURL"
)

var defaultNamespaces = []string{
	alertmanager.PDRegexOS,
	alertmanager.PDRegexLP,
	alertmanager.PDRegexKube,
}

var _ reconcile.Reconciler = &ReconcileSecret{}

// ReconcileSecret reconciles a Secret object
type ReconcileSecret struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	scheme    *runtime.Scheme
	readiness readiness.Interface
}

// Add creates a new Secret Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	client := mgr.GetClient()
	return &ReconcileSecret{
		client:    client,
		scheme:    mgr.GetScheme(),
		readiness: &readiness.Impl{Client: client},
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("secret-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource (type "Secret").
	// For each Add/Update/Delete event, the reconcile loop will be sent a reconcile Request.
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}

// createPagerdutyRoute creates an AlertManager Route for PagerDuty in memory.
func createPagerdutyRoute(namespaceList []string) *alertmanager.Route {
	// order matters.
	// these are sub-routes.  if any matches it will not continue processing.
	// 1. route anything we want to silence to "null"
	// 2. route anything that should be a warning to "make-it-warning"
	// 3. route anything that should be an error to "make-it-error"
	// 4. route anything we want to go to PD
	pagerdutySubroutes := []*alertmanager.Route{
		// Silence anything intended for OCM Agent
		// https://issues.redhat.com/browse/SDE-1315
		{Receiver: receiverNull, Match: map[string]string{managedNotificationLabel: "true"}},
		// https://issues.redhat.com/browse/OSD-1966
		{Receiver: receiverNull, Match: map[string]string{"alertname": "KubeQuotaExceeded"}},
		// This will be renamed in release 4.5
		// https://issues.redhat.com/browse/OSD-4017
		{Receiver: receiverNull, Match: map[string]string{"alertname": "KubeQuotaFullyUsed"}},
		// TODO: Remove CPUThrottlingHigh entry after all OSD clusters upgrade to 4.6 and above version
		// https://issues.redhat.com/browse/OSD-6351 based on https://bugzilla.redhat.com/show_bug.cgi?id=1843346
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CPUThrottlingHigh"}},
		// https://issues.redhat.com/browse/OSD-3010
		{Receiver: receiverNull, Match: map[string]string{"alertname": "NodeFilesystemSpaceFillingUp", "severity": "warning"}},
		// https://issues.redhat.com/browse/OSD-2611
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-customer-monitoring"}},
		// https://issues.redhat.com/browse/OSD-3569
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-operators"}},
		// https://issues.redhat.com/browse/OSD-8337
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-storage"}},
		// https://issues.redhat.com/browse/OSD-8702
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-compliance"}},
		// https://issues.redhat.com/browse/OSD-8349
		{Receiver: receiverNull, Match: map[string]string{"exported_namespace": "openshift-storage"}},
		// https://issues.redhat.com/browse/OSD-6505
		{Receiver: receiverNull, Match: map[string]string{"exported_namespace": "openshift-operators"}},
		// https://issues.redhat.com/browse/OSD-7653
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-operators-redhat"}},
		// https://issues.redhat.com/browse/OSD-3629
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CustomResourceDetected"}},
		// https://issues.redhat.com/browse/OSD-3629
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ImagePruningDisabled"}},
		// https://issues.redhat.com/browse/OSD-3794
		{Receiver: receiverNull, Match: map[string]string{"severity": "info"}},
		// https://issues.redhat.com/browse/OSD-8665
		{Receiver: receiverNull, Match: map[string]string{"alertname": "KubePersistentVolumeFillingUp", "severity": "warning", "namespace": "openshift-logging"}},
		// https://issues.redhat.com/browse/OSD-3973
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "PodDisruptionBudgetLimit"}},
		// https://issues.redhat.com/browse/OSD-3973
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "PodDisruptionBudgetAtLimit"}},
		// https://issues.redhat.com/browse/OSD-4373
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "TargetDown"}},
		// https://issues.redhat.com/browse/OSD-5544
		{Receiver: receiverNull, MatchRE: map[string]string{"job_name": "^elasticsearch.*"}, Match: map[string]string{"alertname": "KubeJobFailed", "namespace": "openshift-logging"}},
		// Suppress the alerts and use HAProxyReloadFailSRE instead (openshift/managed-cluster-config#600)
		{Receiver: receiverNull, Match: map[string]string{"alertname": "HAProxyReloadFail", "severity": "critical"}},
		// https://issues.redhat.com/browse/OHSS-2163
		{Receiver: receiverNull, Match: map[string]string{"alertname": "PrometheusRuleFailures"}},
		// https://issues.redhat.com/browse/OSD-6215
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ClusterOperatorDegraded", "name": "authentication", "reason": "IdentityProviderConfig_Error"}},
		// https://issues.redhat.com/browse/OSD-6363
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ClusterOperatorDegraded", "name": "authentication", "reason": "OAuthServerConfigObservation_Error"}},

		//https://issues.redhat.com/browse/OSD-8320
		// Sometimes only CLusterOperatorDown is firing, meaning the suppression set below in this file does not work
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ClusterOperatorDown", "name": "authentication", "reason": "IdentityProviderConfig_Error"}},
		//https://issues.redhat.com/browse/OSD-8320
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ClusterOperatorDown", "name": "authentication", "reason": "OAuthServerConfigObservation_Error"}},

		// https://issues.redhat.com/browse/OSD-6327
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CannotRetrieveUpdates"}},

		//https://issues.redhat.com/browse/OSD-6559
		{Receiver: receiverNull, Match: map[string]string{"alertname": "PrometheusNotIngestingSamples", "namespace": "openshift-user-workload-monitoring"}},

		//https://issues.redhat.com/browse/OSD-7671
		{Receiver: receiverNull, Match: map[string]string{"alertname": "FluentdQueueLengthBurst", "namespace": "openshift-logging", "severity": "warning"}},
		//https://issues.redhat.com/browse/OSD-8403, https://issues.redhat.com/browse/OSD-8576
		{Receiver: receiverNull, Match: map[string]string{"alertname": "FluentdQueueLengthIncreasing", "namespace": "openshift-logging"}},

		// https://issues.redhat.com/browse/OSD-9061
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ClusterAutoscalerUnschedulablePods", "namespace": "openshift-machine-api"}},

		// https://issues.redhat.com/browse/OSD-9062
		{Receiver: receiverNull, Match: map[string]string{"severity": "alert"}},

		// https://issues.redhat.com/browse/OSD-1922
		{Receiver: receiverMakeItWarning, Match: map[string]string{"alertname": "KubeAPILatencyHigh", "severity": "critical"}},

		// fluentd: route any fluentd alert to PD
		// https://issues.redhat.com/browse/OSD-3326
		{Receiver: receiverPagerduty, Match: map[string]string{"job": "fluentd", "prometheus": "openshift-monitoring/k8s"}},
		{Receiver: receiverPagerduty, Match: map[string]string{"alertname": "FluentdNodeDown", "prometheus": "openshift-monitoring/k8s"}},
		// elasticsearch: route any ES alert to PD
		// https://issues.redhat.com/browse/OSD-3326
		{Receiver: receiverPagerduty, Match: map[string]string{"cluster": "elasticsearch", "prometheus": "openshift-monitoring/k8s"}},

		//Add any alerts below to override their severity to Error

		// Ensure NodeClockNotSynchronising is routed to PD as a high alert
		// https://issues.redhat.com/browse/OSD-8736
		{Receiver: receiverMakeItError, Match: map[string]string{"alertname": "NodeClockNotSynchronising", "prometheus": "openshift-monitoring/k8s"}},

		// Route KubeAPIErrorBudgetBurn to PD despite lack of namespace label
		// https://issues.redhat.com/browse/OSD-8006
		{Receiver: receiverPagerduty, Match: map[string]string{"alertname": "KubeAPIErrorBudgetBurn", "prometheus": "openshift-monitoring/k8s"}},

		// https://issues.redhat.com/browse/OSD-6821
		{Receiver: receiverNull, Match: map[string]string{"alertname": "PrometheusBadConfig", "namespace": "openshift-user-workload-monitoring"}},
		{Receiver: receiverNull, Match: map[string]string{"alertname": "PrometheusDuplicateTimestamps", "namespace": "openshift-user-workload-monitoring"}},

		// https://issues.redhat.com/browse/OSD-9426
		{Receiver: receiverNull, Match: map[string]string{"alertname": "PrometheusTargetSyncFailure", "namespace": "openshift-user-workload-monitoring"}},

		// https://issues.redhat.com/browse/OSD-8983
		{Receiver: receiverMakeItWarning, Match: map[string]string{"alertname": "etcdGRPCRequestsSlow", "namespace": "openshift-etcd"}},

		// https://issues.redhat.com/browse/OSD-10473
		{Receiver: receiverMakeItWarning, Match: map[string]string{"alertname": "ExtremelyHighIndividualControlPlaneCPU", "namespace": "openshift-kube-apiserver"}},

		// https://issues.redhat.com/browse/OSD-10485
		{Receiver: receiverMakeItWarning, Match: map[string]string{"alertname": "etcdHighNumberOfFailedGRPCRequests", "namespace": "openshift-etcd"}},

		// https://issues.redhat.com/browse/OSD-11298
		{Receiver: receiverMakeItCritical, MatchRE: map[string]string{"name": ".*master.*"}, Match: map[string]string{"alertname": "MachineWithoutValidNode", "namespace": "openshift-machine-api"}},
		{Receiver: receiverMakeItCritical, MatchRE: map[string]string{"name": ".*master.*"}, Match: map[string]string{"alertname": "MachineWithNoRunningPhase", "namespace": "openshift-machine-api"}},
	}

	for _, namespace := range namespaceList {
		pagerdutySubroutes = append(pagerdutySubroutes, []*alertmanager.Route{
			// https://issues.redhat.com/browse/OSD-3086
			// https://issues.redhat.com/browse/OSD-5872
			{Receiver: receiverPagerduty, MatchRE: map[string]string{"exported_namespace": namespace}, Match: map[string]string{"prometheus": "openshift-monitoring/k8s"}},
			// general: route anything in core namespaces to PD
			{Receiver: receiverPagerduty, MatchRE: map[string]string{"namespace": namespace}, Match: map[string]string{"exported_namespace": "", "prometheus": "openshift-monitoring/k8s"}},
		}...)
	}

	return &alertmanager.Route{
		Receiver: defaultReceiver,
		GroupByStr: []string{
			"alertname",
			"severity",
		},
		Continue: true,
		Routes:   pagerdutySubroutes,
	}
}

// createOCMAgentRoute creates an AlertManager Route for OcmAgent in memory.
func createOCMAgentRoute() *alertmanager.Route {
	return &alertmanager.Route{
		Receiver:       receiverOCMAgent,
		Continue:       false,
		Match:          map[string]string{managedNotificationLabel: "true"},
		RepeatInterval: "10m",
	}
}

// createOCMAgentReceiver creates an AlertManager Receiver for OCM Agent in memory.
func createOCMAgentReceiver(ocmAgentURL string) []*alertmanager.Receiver {
	if ocmAgentURL == "" {
		return []*alertmanager.Receiver{}
	}

	ocmAgentConfig := &alertmanager.WebhookConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		URL:            ocmAgentURL,
	}

	return []*alertmanager.Receiver{
		{
			Name:           receiverOCMAgent,
			WebhookConfigs: []*alertmanager.WebhookConfig{ocmAgentConfig},
		},
	}
}

// createPagerdutyConfig creates an AlertManager PagerdutyConfig for PagerDuty in memory.
func createPagerdutyConfig(pagerdutyRoutingKey, clusterID string, clusterProxy string) *alertmanager.PagerdutyConfig {
	detailsMap := map[string]string{
		"alert_name":   `{{ .CommonLabels.alertname }}`,
		"link":         `{{ if .CommonAnnotations.runbook_url }}{{ .CommonAnnotations.runbook_url }}{{ else if .CommonAnnotations.link }}{{ .CommonAnnotations.link }}{{ else }}https://github.com/openshift/ops-sop/tree/master/v4/alerts/{{ .CommonLabels.alertname }}.md{{ end }}`,
		"ocm_link":     fmt.Sprintf("https://console.redhat.com/openshift/details/%s", clusterID),
		"num_firing":   `{{ .Alerts.Firing | len }}`,
		"num_resolved": `{{ .Alerts.Resolved | len }}`,
		"resolved":     `{{ template "pagerduty.default.instances" .Alerts.Resolved }}`,
		"cluster_id":   clusterID,
	}

	if config.IsFedramp() {
		detailsMap["ocm_link"] = ``
		detailsMap["resolved"] = ``
		detailsMap["cluster_id"] = ``
		detailsMap["firing"] = ``
		detailsMap["link"] = ``
	}

	return &alertmanager.PagerdutyConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		RoutingKey:     pagerdutyRoutingKey,
		Severity:       `{{ if .CommonLabels.severity }}{{ .CommonLabels.severity | toLower }}{{ else }}critical{{ end }}`,
		Description:    `{{ .CommonLabels.alertname }} {{ .CommonLabels.severity | toUpper }} ({{ len .Alerts }})`,
		Details:        detailsMap,
		HttpConfig:     createHttpConfig(clusterProxy),
	}

}

// createPagerdutyReceivers creates an AlertManager Receiver for PagerDuty in memory.
func createPagerdutyReceivers(pagerdutyRoutingKey, clusterID string, clusterProxy string) []*alertmanager.Receiver {
	if pagerdutyRoutingKey == "" {
		return []*alertmanager.Receiver{}
	}

	receivers := []*alertmanager.Receiver{
		{
			Name:             receiverPagerduty,
			PagerdutyConfigs: []*alertmanager.PagerdutyConfig{createPagerdutyConfig(pagerdutyRoutingKey, clusterID, clusterProxy)},
		},
	}

	// make-it-warning overrides the severity
	pdconfig := createPagerdutyConfig(pagerdutyRoutingKey, clusterID, clusterProxy)
	pdconfig.Severity = "warning"
	receivers = append(receivers, &alertmanager.Receiver{
		Name:             receiverMakeItWarning,
		PagerdutyConfigs: []*alertmanager.PagerdutyConfig{pdconfig},
	})

	// make-it-error overrides the severity
	highpdconfig := createPagerdutyConfig(pagerdutyRoutingKey, clusterID, clusterProxy)
	highpdconfig.Severity = "error"
	receivers = append(receivers, &alertmanager.Receiver{
		Name:             receiverMakeItError,
		PagerdutyConfigs: []*alertmanager.PagerdutyConfig{highpdconfig},
	})

	// make-it-error overrides the severity
	criticalpdconfig := createPagerdutyConfig(pagerdutyRoutingKey, clusterID, clusterProxy)
	criticalpdconfig.Severity = "critical"
	receivers = append(receivers, &alertmanager.Receiver{
		Name:             receiverMakeItCritical,
		PagerdutyConfigs: []*alertmanager.PagerdutyConfig{criticalpdconfig},
	})

	return receivers
}

// createWatchdogRoute creates an AlertManager Route for Watchdog (Dead Man's Snitch) in memory.
func createWatchdogRoute() *alertmanager.Route {
	return &alertmanager.Route{
		Receiver:       receiverWatchdog,
		RepeatInterval: "5m",
		Match:          map[string]string{"alertname": "Watchdog"},
	}
}

// createWatchdogReceivers creates an AlertManager Receiver for Watchdog (Dead Man's Snitch) in memory.
func createWatchdogReceivers(watchdogURL string, clusterProxy string) []*alertmanager.Receiver {
	if watchdogURL == "" {
		return []*alertmanager.Receiver{}
	}

	snitchconfig := &alertmanager.WebhookConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		URL:            watchdogURL,
		HttpConfig:     createHttpConfig(clusterProxy),
	}

	return []*alertmanager.Receiver{
		{
			Name:           receiverWatchdog,
			WebhookConfigs: []*alertmanager.WebhookConfig{snitchconfig},
		},
	}
}

// createHttpConfig creates a HttpConfig used for receivers that can accept that configuration
func createHttpConfig(clusterProxy string) alertmanager.HttpConfig {
	if clusterProxy == "" {
		return alertmanager.HttpConfig{}
	}
	return alertmanager.HttpConfig{
		ProxyURL:  clusterProxy,
		TLSConfig: alertmanager.TLSConfig{},
	}
}

// createAlertManagerConfig creates an AlertManager Config in memory based on the provided input parameters.
func createAlertManagerConfig(pagerdutyRoutingKey, watchdogURL, ocmAgentURL, clusterID string, clusterProxy string, namespaceList []string) *alertmanager.Config {
	routes := []*alertmanager.Route{}
	receivers := []*alertmanager.Receiver{}

	if watchdogURL != "" {
		routes = append(routes, createWatchdogRoute())
		receivers = append(receivers, createWatchdogReceivers(watchdogURL, clusterProxy)...)
	}

	if ocmAgentURL != "" {
		routes = append(routes, createOCMAgentRoute())
		receivers = append(receivers, createOCMAgentReceiver(ocmAgentURL)...)
	}

	if pagerdutyRoutingKey != "" {
		routes = append(routes, createPagerdutyRoute(namespaceList))
		receivers = append(receivers, createPagerdutyReceivers(pagerdutyRoutingKey, clusterID, clusterProxy)...)
	}

	// always have the "null" receiver
	receivers = append(receivers, &alertmanager.Receiver{Name: receiverNull})

	amconfig := &alertmanager.Config{
		Global: &alertmanager.GlobalConfig{
			ResolveTimeout: "5m",
			PagerdutyURL:   pagerdutyURL,
		},
		Route: &alertmanager.Route{
			Receiver: defaultReceiver,
			GroupByStr: []string{
				"job",
			},
			GroupWait:      "30s",
			GroupInterval:  "5m",
			RepeatInterval: "12h",
			Routes:         routes,
		},
		Receivers: receivers,
		Templates: []string{},
		// Work request: https://issues.redhat.com/browse/OSD-4623
		// Reference: https://github.com/openshift/cluster-monitoring-operator/blob/6a02b14773169330d7a31ede73dce5adb1c66bb4/assets/alertmanager/secret.yaml
		InhibitRules: []*alertmanager.InhibitRule{
			{
				// Critical alert shouldn't also alert for warning/info
				Equal: []string{
					"namespace",
					"alertname",
				},
				SourceMatch: map[string]string{
					"severity": "critical",
				},
				TargetMatchRE: map[string]string{
					"severity": "warning|info",
				},
			},
			{
				// Warning alerts shouldn't also alert for info
				Equal: []string{
					"namespace",
					"alertname",
				},
				SourceMatch: map[string]string{
					"severity": "warning",
				},
				TargetMatchRE: map[string]string{
					"severity": "info",
				},
			},
			{
				// If a cluster operator is degraded, don't also fire ClusterOperatorDown
				// The degraded alert is critical, and usually has more details
				Equal: []string{
					"namespace",
					"name",
				},
				SourceMatch: map[string]string{
					"alertname": "ClusterOperatorDegraded",
					"severity":  "critical",
				},
				TargetMatchRE: map[string]string{
					"alertname": "ClusterOperatorDown",
				},
			},
			{
				// If a node is not ready, we already know it's Unreachable
				Equal: []string{
					"node",
					"instance",
				},
				SourceMatch: map[string]string{
					"alertname": "KubeNodeNotReady",
				},
				TargetMatchRE: map[string]string{
					"alertname": "KubeNodeUnreachable",
				},
			},
			{
				// node being Unreachable may also trigger certain pods being unavailable
				SourceMatch: map[string]string{
					"alertname": "KubeNodeUnreachable",
				},
				TargetMatchRE: map[string]string{
					"alertname": "SDNPodNotReady|TargetDown",
				},
			},
			{
				// If a node is NotReady, then we also know that there will be pods that aren't running
				Equal: []string{
					"instance",
				},
				SourceMatch: map[string]string{
					"alertname": "KubeNodeNotReady",
				},
				TargetMatchRE: map[string]string{
					"alertname": "KubeDaemonSetRolloutStuck|KubeDaemonSetMisScheduled|KubeDeploymentReplicasMismatch|KubeStatefulSetReplicasMismatch|KubePodNotReady",
				},
			},
			{
				// If a deployment doesn't have it's replicas mismatch, then we don't need to fire for the pod not being ready
				Equal: []string{
					"namespace",
				},
				SourceMatch: map[string]string{
					"alertname": "KubeDeploymentReplicasMismatch",
				},
				TargetMatchRE: map[string]string{
					"alertname": "KubePodNotReady|KubePodCrashLooping",
				},
			},
			{
				// NB this label obviously won't match and that's both ok and expected. When a label is missing (or empty) on both source and target, the rule will apply (see: docs ).
				// see: https://www.prometheus.io/docs/alerting/latest/configuration/#inhibit_rule

				// If there wasn't a label here, the tests exploded spectacularly, so I figured a label that would never match is the next best thing.
				Equal: []string{
					"dummylabel",
				},
				SourceMatch: map[string]string{
					"alertname": "ElasticsearchOperatorCSVNotSuccessful",
				},
				TargetMatchRE: map[string]string{
					"alertname": "ElasticsearchClusterNotHealthy",
				},
			},
		},
	}

	return amconfig
}

// Retrieves data from all relevant configMaps. Returns a list of namespaces, represented as regular expressions, to monitor
func (r *ReconcileSecret) parseConfigMaps(reqLogger logr.Logger, cmList *corev1.ConfigMapList, cmNamespace string) (namespaceList []string) {
	// Retrieve namespaces from their respective configMaps, if the configMaps exist
	managedNamespaces := r.parseNamespaceConfigMap(reqLogger, cmNameManagedNamespaces, cmNamespace, cmKeyManagedNamespaces, cmList)
	ocpNamespaces := r.parseNamespaceConfigMap(reqLogger, cmNameOCPNamespaces, cmNamespace, cmKeyOCPNamespaces, cmList)
	addonsNamespaces := r.parseNamespaceConfigMap(reqLogger, cmNameAddonsNamespaces, cmNamespace, cmKeyAddonsNamespaces, cmList)

	// Default to alerting on all ^openshift-.* namespaces if either list is empty, potentially indicating a problem parsing configMaps
	if len(managedNamespaces) == 0 ||
		len(ocpNamespaces) == 0 ||
		len(addonsNamespaces) == 0 {
		reqLogger.Info("DEBUG: Could not retrieve namespaces from one or more configMaps. Using default namespaces", "Default namespaces", defaultNamespaces)
		return defaultNamespaces
	}

	namespaceList = append(namespaceList, managedNamespaces...)
	namespaceList = append(namespaceList, ocpNamespaces...)
	namespaceList = append(namespaceList, addonsNamespaces...)

	return namespaceList
}

// Returns the namespaces from a *-namespaces configMap as a list of regular expressions
func (r *ReconcileSecret) parseNamespaceConfigMap(reqLogger logr.Logger, cmName string, cmNamespace string, cmKey string, cmList *corev1.ConfigMapList) (nsList []string) {
	cmExists := cmInList(reqLogger, cmName, cmList)
	if !cmExists {
		reqLogger.Info("INFO: ConfigMap does not exist", "ConfigMap", cmNameManagedNamespaces)
		return []string{}
	}

	// Unmarshal configMap, fail on error or if no namespaces are present in decoded config
	var namespaceConfig alertmanager.NamespaceConfig
	rawNamespaces := readCMKey(r, reqLogger, cmName, cmNamespace, cmKey)
	err := yaml.Unmarshal([]byte(rawNamespaces), &namespaceConfig)
	if err != nil {
		reqLogger.Info("DEBUG: Unable to unmarshal from configMap", "ConfigMap", fmt.Sprintf("%s/%s", cmNamespace, cmName), "Error", err)
		return []string{}
	} else if len(namespaceConfig.Resources.Namespaces) == 0 {
		reqLogger.Info("DEBUG: No namespaces found in configMap", "ConfigMap", fmt.Sprintf("%s/%s", cmNamespace, cmName))
		return []string{}
	}

	for _, ns := range namespaceConfig.Resources.Namespaces {
		nsList = append(nsList, "^"+ns.Name+"$")
	}
	return nsList
}

// readOCMAgentServiceURLFromConfig returns the OCM Agent service URL from the OCM Agent configmap
func (r *ReconcileSecret) readOCMAgentServiceURLFromConfig(reqLogger logr.Logger, cmList *corev1.ConfigMapList, cmNamespace string) string {
	cmExists := cmInList(reqLogger, cmNameOcmAgent, cmList)
	if !cmExists {
		log.Info("INFO: ConfigMap does not exist", "ConfigMap", cmNameOcmAgent)
		return ""
	}

	// Unmarshal configMap, fail on error or if no namespaces are present in decoded config
	serviceURL := readCMKey(r, reqLogger, cmNameOcmAgent, cmNamespace, cmKeyOCMAgent)
	if _, err := url.ParseRequestURI(serviceURL); err != nil {
		log.Error(err, "Invalid OCM Agent Service URL")
		return ""
	}

	return serviceURL
}

func (r *ReconcileSecret) parseSecrets(reqLogger logr.Logger, secretList *corev1.SecretList, namespace string, clusterReady bool) (pagerdutyRoutingKey string, watchdogURL string) {
	// Check for the presence of specific secrets.
	pagerDutySecretExists := secretInList(reqLogger, secretNamePD, secretList)
	snitchSecretExists := secretInList(reqLogger, secretNameDMS, secretList)

	// do the work! collect secret info for PD and DMS
	pagerdutyRoutingKey = ""
	watchdogURL = ""

	// If a secret exists, add the necessary configs to Alertmanager.
	// But don't activate PagerDuty unless the cluster is "ready".
	// This is to avoid alert noise while the cluster is still being installed and configured.
	if pagerDutySecretExists {
		reqLogger.Info("INFO: Pager Duty secret exists")
		if clusterReady {
			reqLogger.Info("INFO: Cluster is ready; configuring Pager Duty")
			pagerdutyRoutingKey = readSecretKey(r, secretNamePD, namespace, secretKeyPD)
		} else {
			reqLogger.Info("INFO: Cluster is not ready; skipping Pager Duty configuration")
		}
	} else {
		reqLogger.Info("INFO: Pager Duty secret does not exist")
	}

	if snitchSecretExists {
		reqLogger.Info("INFO: Dead Man's Snitch secret exists")
		watchdogURL = readSecretKey(r, secretNameDMS, namespace, secretKeyDMS)
	} else {
		reqLogger.Info("INFO: Dead Man's Snitch secret does not exist")
	}

	return pagerdutyRoutingKey, watchdogURL
}

// Reconcile reads that state of the cluster for a Secret object and makes changes based on the state read.
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSecret) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if request.Namespace != config.OperatorNamespace {
		return reconcile.Result{}, nil
	}

	reqLogger := log.WithValues("Request.Name", request.Name)
	reqLogger.Info("Reconciling Object")

	// This operator is only interested in the 3 secrets & 1 configMap listed below. Skip reconciling for all other objects.
	// TODO: Filter these with a predicate instead
	switch request.Name {
	case secretNamePD:
	case secretNameDMS:
	case secretNameAlertmanager:
	case cmNameOcmAgent:
	case cmNameManagedNamespaces:
	case cmNameOCPNamespaces:
	case cmNameAddonsNamespaces:
	default:
		reqLogger.Info("Skip reconcile: No changes detected to alertmanager secrets.")
		return reconcile.Result{}, nil
	}
	reqLogger.Info("DEBUG: Started reconcile loop")

	clusterReady, err := r.readiness.IsReady()
	if err != nil {
		reqLogger.Error(err, "Error determining cluster readiness.")
		return r.readiness.Result(), err
	}

	// Get a list of all relevant objects in the `openshift-monitoring` namespace.
	// This is used for determining which secrets and configMaps are present so that the necessary
	// Alertmanager config changes can happen later.
	opts := []client.ListOption{
		client.InNamespace(request.Namespace),
	}
	secretList := &corev1.SecretList{}
	err = r.client.List(context.TODO(), secretList, opts...)
	if err != nil {
		reqLogger.Error(err, "Unable to list secrets")
	}

	cmList := &corev1.ConfigMapList{}
	err = r.client.List(context.TODO(), cmList, opts...)
	if err != nil {
		reqLogger.Error(err, "Unable to list configMaps")
	}

	pagerdutyRoutingKey, watchdogURL := r.parseSecrets(reqLogger, secretList, request.Namespace, clusterReady)
	osdNamespaces := r.parseConfigMaps(reqLogger, cmList, request.Namespace)
	reqLogger.Info("DEBUG: Adding PagerDuty routes for the following namespaces", "Namespaces", osdNamespaces)

	ocmAgentURL := r.readOCMAgentServiceURLFromConfig(reqLogger, cmList, request.Namespace)

	clusterProxy, err := r.getClusterProxy()
	if err != nil {
		reqLogger.Error(err, "Unable to get cluster proxy")
	}

	// create the desired alertmanager Config
	clusterID, err := r.getClusterID()
	if err != nil {
		reqLogger.Error(err, "Error reading cluster id.")
	}
	alertmanagerconfig := createAlertManagerConfig(pagerdutyRoutingKey, watchdogURL, ocmAgentURL, clusterID, clusterProxy, osdNamespaces)

	// write the alertmanager Config
	writeAlertManagerConfig(r, reqLogger, alertmanagerconfig)

	// Update metrics after all reconcile operations are complete.
	metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
	metrics.UpdateConfigMapMetrics(cmList)
	reqLogger.Info("Finished reconcile for secret.")

	// The readiness Result decides whether we should requeue, effectively "polling" the readiness logic.
	return r.readiness.Result(), nil
}

func (r *ReconcileSecret) getClusterID() (string, error) {
	var version configv1.ClusterVersion
	err := r.client.Get(context.TODO(), client.ObjectKey{Name: "version"}, &version)
	if err != nil {
		return "", err
	}
	return string(version.Spec.ClusterID), nil
}

func (r *ReconcileSecret) getClusterProxy() (string, error) {
	var proxy configv1.Proxy
	err := r.client.Get(context.TODO(), client.ObjectKey{Name: "cluster"}, &proxy)
	if err != nil {
		return "", err
	}
	// Only care about HTTPS proxy, as PD and DMS comms will be HTTPS
	if proxy.Status.HTTPSProxy != "" {
		return proxy.Status.HTTPSProxy, nil
	}
	return "", nil
}

// secretInList takes the name of Secret, and a list of Secrets, and returns a Bool
// indicating if the name is present in the list
func secretInList(reqLogger logr.Logger, name string, list *corev1.SecretList) bool {
	for _, secret := range list.Items {
		if name == secret.Name {
			reqLogger.Info(fmt.Sprintf("DEBUG: Secret named '%s' found", secret.Name))
			return true
		}
	}
	reqLogger.Info(fmt.Sprintf("DEBUG: Secret named '%s' not found", name))
	return false
}

// cmInList takes the name of ConfigMap, and a list of ConfigMaps, and returns a Bool
// indicating if the name is present in the list
func cmInList(reqLogger logr.Logger, name string, list *corev1.ConfigMapList) bool {
	for _, cm := range list.Items {
		if name == cm.Name {
			reqLogger.Info(fmt.Sprintf("DEBUG: ConfigMap named '%s' found", cm.Name))
			return true
		}
	}
	reqLogger.Info(fmt.Sprintf("DEBUG: ConfigMap named '%s' found", name))
	return false
}

// readCMKey fetches the data from a ConfigMap, such as the managed namespace list
func readCMKey(r *ReconcileSecret, reqLogger logr.Logger, cmName string, cmNamespace string, fieldName string) string {

	configMap := &corev1.ConfigMap{}

	// Define a new objectKey for fetching the secret key.
	objectKey := client.ObjectKey{
		Namespace: cmNamespace,
		Name:      cmName,
	}

	// Fetch the key from the secret object.
	// TODO: Check error from Get(). Right now secret.Data[fieldname] will panic.
	err := r.client.Get(context.TODO(), objectKey, configMap)
	if err != nil {
		reqLogger.Error(err, "Error: Failed to retrieve configMap", "Name", cmName)
	}
	return string(configMap.Data[fieldName])
}

// readSecretKey fetches the data from a Secret, such as a PagerDuty API key.
func readSecretKey(r *ReconcileSecret, secretName string, secretNamespace string, fieldName string) string {

	secret := &corev1.Secret{}

	// Define a new objectKey for fetching the secret key.
	objectKey := client.ObjectKey{
		Namespace: secretNamespace,
		Name:      secretName,
	}

	// Fetch the key from the secret object.
	// TODO: Check error from Get(). Right now secret.Data[fieldname] will panic.
	_ = r.client.Get(context.TODO(), objectKey, secret)
	return string(secret.Data[fieldName])
}

// writeAlertManagerConfig writes the updated alertmanager config to the `alertmanager-main` secret in namespace `openshift-monitoring`.
func writeAlertManagerConfig(r *ReconcileSecret, reqLogger logr.Logger, amconfig *alertmanager.Config) {
	amconfigbyte, marshalerr := yaml.Marshal(amconfig)
	if marshalerr != nil {
		reqLogger.Error(marshalerr, "ERROR: failed to marshal Alertmanager config")
	}
	// This is commented out because it prints secrets, but it might be useful for debugging when running locally.
	//reqLogger.Info("DEBUG: Marshalled Alertmanager config:", string(amconfigbyte))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretNameAlertmanager,
			Namespace: "openshift-monitoring",
		},
		Data: map[string][]byte{
			"alertmanager.yaml": amconfigbyte,
		},
	}

	// Write the alertmanager config into the alertmanager secret.
	err := r.client.Update(context.TODO(), secret)
	if err != nil {
		if errors.IsNotFound(err) {
			// couldn't update because it didn't exist.
			// create it instead.
			err = r.client.Create(context.TODO(), secret)
		}
	}

	if err != nil {
		reqLogger.Error(err, "ERROR: Could not write secret alertmanger-main", "namespace", secret.Namespace)
		return
	}
	reqLogger.Info("INFO: Secret alertmanager-main successfully updated")
}
