package secret

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/configure-alertmanager-operator/config"
	"github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"

	configv1 "github.com/openshift/api/config/v1"
	yaml "gopkg.in/yaml.v2"
)

var log = logf.Log.WithName("secret_controller")

const (
	secretKeyPD = "PAGERDUTY_KEY"

	secretKeyDMS = "SNITCH_URL"

	secretNamePD = "pd-secret"

	secretNameDMS = "dms-secret"

	secretNameAlertmanager = "alertmanager-main"

	// anything routed to "null" receiver does not get routed to PD
	receiverNull = "null"

	// anything routed to "make-it-warning" receiver has severity=warning
	receiverMakeItWarning = "make-it-warning"

	// anything routed to "pagerduty" will alert/notify SREP
	receiverPagerduty = "pagerduty"

	// anything going to Dead Man's Snitch (watchdog)
	receiverWatchdog = "watchdog"

	// the default receiver used by the route used for pagerduty
	defaultReceiver = receiverNull

	// global config for PagerdutyURL
	pagerdutyURL = "https://events.pagerduty.com/v2/enqueue"

	openShiftConfigManagedNamespaceName = "openshift-config-managed"
	consolePublicConfigMap              = "console-public"
)

var _ reconcile.Reconciler = &ReconcileSecret{}

// ReconcileSecret reconciles a Secret object
type ReconcileSecret struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Add creates a new Secret Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileSecret{client: mgr.GetClient(), scheme: mgr.GetScheme()}
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
	return nil
}

// createPagerdutyRoute creates an AlertManager Route for PagerDuty in memory.
func createPagerdutyRoute() *alertmanager.Route {
	// order matters.
	// these are sub-routes.  if any matches it will not continue processing.
	// 1. route anything we want to silence to "null"
	// 2. route anything that should be a warning to "make-it-warning"
	// 3. route anything we want to go to PD
	pagerdutySubroutes := []*alertmanager.Route{
		// https://issues.redhat.com/browse/OSD-1966
		{Receiver: receiverNull, Match: map[string]string{"alertname": "KubeQuotaExceeded"}},
		// This will be renamed in release 4.5
		// https://issues.redhat.com/browse/OSD-4017
		{Receiver: receiverNull, Match: map[string]string{"alertname": "KubeQuotaFullyUsed"}},
		// https://issues.redhat.com/browse/OSD-2382
		{Receiver: receiverNull, Match: map[string]string{"alertname": "UsingDeprecatedAPIAppsV1Beta1"}},
		// https://issues.redhat.com/browse/OSD-2382
		{Receiver: receiverNull, Match: map[string]string{"alertname": "UsingDeprecatedAPIAppsV1Beta2"}},
		// https://issues.redhat.com/browse/OSD-2382
		{Receiver: receiverNull, Match: map[string]string{"alertname": "UsingDeprecatedAPIExtensionsV1Beta1"}},
		// https://issues.redhat.com/browse/OSD-2980
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CPUThrottlingHigh", "container": "registry-server"}},
		// https://issues.redhat.com/browse/OSD-3008
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CPUThrottlingHigh", "container": "configmap-registry-server"}},
		// https://issues.redhat.com/browse/OSD-3010
		{Receiver: receiverNull, Match: map[string]string{"alertname": "NodeFilesystemSpaceFillingUp", "severity": "warning"}},
		// https://issues.redhat.com/browse/OSD-2611
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-customer-monitoring"}},
		// https://issues.redhat.com/browse/OSD-3569
		{Receiver: receiverNull, Match: map[string]string{"namespace": "openshift-operators"}},
		// https://issues.redhat.com/browse/OSD-3220
		{Receiver: receiverNull, Match: map[string]string{"alertname": "SLAUptimeSRE"}},
		// https://issues.redhat.com/browse/OSD-3629
		{Receiver: receiverNull, Match: map[string]string{"alertname": "CustomResourceDetected"}},
		// https://issues.redhat.com/browse/OSD-3629
		{Receiver: receiverNull, Match: map[string]string{"alertname": "ImagePruningDisabled"}},
		// https://issues.redhat.com/browse/OSD-3794
		{Receiver: receiverNull, Match: map[string]string{"severity": "info"}},
		// https://issues.redhat.com/browse/OSD-4631
		{Receiver: receiverNull, MatchRE: map[string]string{"alertname": "^etcd.*"}, Match: map[string]string{"severity": "warning"}},
		// https://issues.redhat.com/browse/OSD-3973
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "PodDisruptionBudgetLimit"}},
		// https://issues.redhat.com/browse/OSD-3973
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "PodDisruptionBudgetAtLimit"}},
		// https://issues.redhat.com/browse/OSD-4265, https://issues.redhat.com/browse/INTLY-8790
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "CPUThrottlingHigh"}},
		// https://issues.redhat.com/browse/OSD-4373
		{Receiver: receiverNull, MatchRE: map[string]string{"namespace": alertmanager.PDRegexLP}, Match: map[string]string{"alertname": "TargetDown"}},

		// https://issues.redhat.com/browse/OSD-1922
		{Receiver: receiverMakeItWarning, Match: map[string]string{"alertname": "KubeAPILatencyHigh", "severity": "critical"}},

		// https://issues.redhat.com/browse/OSD-3086
		{Receiver: receiverPagerduty, MatchRE: map[string]string{"exported_namespace": alertmanager.PDRegex}},
		// general: route anything in core namespaces to PD
		{Receiver: receiverPagerduty, MatchRE: map[string]string{"namespace": alertmanager.PDRegex}, Match: map[string]string{"exported_namespace": ""}},
		// fluentd: route any fluentd alert to PD
		// https://issues.redhat.com/browse/OSD-3326
		{Receiver: receiverPagerduty, Match: map[string]string{"job": "fluentd"}},
		{Receiver: receiverPagerduty, Match: map[string]string{"alertname": "FluentdNodeDown"}},
		// elasticsearch: route any ES alert to PD
		// https://issues.redhat.com/browse/OSD-3326
		{Receiver: receiverPagerduty, Match: map[string]string{"cluster": "elasticsearch"}},
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

// createPagerdutyConfig creates an AlertManager PagerdutyConfig for PagerDuty in memory.
func createPagerdutyConfig(pagerdutyRoutingKey, consoleUrl string, clusterID string) *alertmanager.PagerdutyConfig {
	return &alertmanager.PagerdutyConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		RoutingKey:     pagerdutyRoutingKey,
		Severity:       `{{ if .CommonLabels.severity }}{{ .CommonLabels.severity | toLower }}{{ else }}critical{{ end }}`,
		Description:    `{{ .CommonLabels.alertname }} {{ .CommonLabels.severity | toUpper }} ({{ len .Alerts }})`,
		Details: map[string]string{
			"link":         `{{ if .CommonAnnotations.link }}{{ .CommonAnnotations.link }}{{ else }}https://github.com/openshift/ops-sop/tree/master/v4/alerts/{{ .CommonLabels.alertname }}.md{{ end }}`,
			"link2":        `{{ if .CommonAnnotations.runbook }}{{ .CommonAnnotations.runbook }}{{ else }}{{ end }}`,
			"console":      consoleUrl,
			"group":        `{{ .CommonLabels.alertname }}`,
			"component":    `{{ .CommonLabels.alertname }}`,
			"num_firing":   `{{ .Alerts.Firing | len }}`,
			"num_resolved": `{{ .Alerts.Resolved | len }}`,
			"resolved":     `{{ template "pagerduty.default.instances" .Alerts.Resolved }}`,
			"cluster_id":   clusterID,
		},
	}

}

// createPagerdutyReceivers creates an AlertManager Receiver for PagerDuty in memory.
func createPagerdutyReceivers(pagerdutyRoutingKey, consoleUrl string, clusterID string) []*alertmanager.Receiver {
	if pagerdutyRoutingKey == "" {
		return []*alertmanager.Receiver{}
	}

	receivers := []*alertmanager.Receiver{
		{
			Name:             receiverPagerduty,
			PagerdutyConfigs: []*alertmanager.PagerdutyConfig{createPagerdutyConfig(pagerdutyRoutingKey, consoleUrl, clusterID)},
		},
	}

	// make-it-warning overrides the severity
	pdconfig := createPagerdutyConfig(pagerdutyRoutingKey, consoleUrl, clusterID)
	pdconfig.Severity = "warning"
	receivers = append(receivers, &alertmanager.Receiver{
		Name:             receiverMakeItWarning,
		PagerdutyConfigs: []*alertmanager.PagerdutyConfig{pdconfig},
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

// createWatchdogReceivers creates an AlertManager Receiver for Watchdog (Dead Man's Sntich) in memory.
func createWatchdogReceivers(watchdogURL string) []*alertmanager.Receiver {
	if watchdogURL == "" {
		return []*alertmanager.Receiver{}
	}

	snitchconfig := &alertmanager.WebhookConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		URL:            watchdogURL,
	}

	return []*alertmanager.Receiver{
		{
			Name:           receiverWatchdog,
			WebhookConfigs: []*alertmanager.WebhookConfig{snitchconfig},
		},
	}
}

// createAlertManagerConfig creates an AlertManager Config in memory based on the provided input parameters.
func createAlertManagerConfig(pagerdutyRoutingKey, watchdogURL, consoleUrl string, clusterID string) *alertmanager.Config {
	routes := []*alertmanager.Route{}
	receivers := []*alertmanager.Receiver{}

	if pagerdutyRoutingKey != "" {
		routes = append(routes, createPagerdutyRoute())
		receivers = append(receivers, createPagerdutyReceivers(pagerdutyRoutingKey, consoleUrl, clusterID)...)
	}

	if watchdogURL != "" {
		routes = append(routes, createWatchdogRoute())
		receivers = append(receivers, createWatchdogReceivers(watchdogURL)...)
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
				Equal: []string{
					"namespace",
					"name",
				},
				SourceMatch: map[string]string{
					"alertname": "ClusterOperatorDown",
				},
				TargetMatchRE: map[string]string{
					"alertname": "ClusterOperatorDegraded",
				},
			},
			{
				Equal: []string{
					"namespace",
					"instance",
				},
				SourceMatch: map[string]string{
					"alertname": "KubeNodeNotReady",
				},
				TargetMatchRE: map[string]string{
					"alertname": "KubeDaemonSetRolloutStuck",
				},
			},
		},
	}

	return amconfig
}

func (r *ReconcileSecret) getWebConsoleUrl() (string, error) {
	consolePublicConfig := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: openShiftConfigManagedNamespaceName, Name: consolePublicConfigMap}, consolePublicConfig)
	if err != nil {
		return "", fmt.Errorf("unable to get console configmap: %v", err)
	}

	consoleUrl, exists := consolePublicConfig.Data["consoleURL"]
	if !exists {
		return "", fmt.Errorf("unable to determine console location from the configmap")
	}
	return consoleUrl, nil
}

// Reconcile reads that state of the cluster for a Secret object and makes changes based on the state read.
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSecret) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if request.Namespace != config.OperatorNamespace {
		return reconcile.Result{}, nil
	}

	reqLogger := log.WithValues("Request.Name", request.Name)
	reqLogger.Info("Reconciling Secret")

	// This operator is only interested in the 3 secrets listed below. Skip reconciling for all other secrets.
	switch request.Name {
	case secretNamePD:
	case secretNameDMS:
	case secretNameAlertmanager:
	default:
		reqLogger.Info("Skip reconcile: No changes detected to alertmanager secrets.")
		return reconcile.Result{}, nil
	}
	log.Info("DEBUG: Started reconcile loop")

	// Get a list of all Secrets in the `openshift-monitoring` namespace.
	// This is used for determining which secrets are present so that the necessary
	// Alertmanager config changes can happen later.
	secretList := &corev1.SecretList{}
	opts := []client.ListOption{
		client.InNamespace(request.Namespace),
	}
	r.client.List(context.TODO(), secretList, opts...)

	// Check for the presence of specific secrets.
	pagerDutySecretExists := secretInList(secretNamePD, secretList)
	snitchSecretExists := secretInList(secretNameDMS, secretList)

	// Get the secret from the request.  If it's a secret we monitor, flag for reconcile.
	instance := &corev1.Secret{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)

	// if there was an error other than "not found" requeue
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("INFO: This secret has been deleted", "name", request.Name)
		} else {
			// Error and requeue in all other circumstances.
			// Don't requeue if a Secret is not found. It's valid to have an absent Pager Duty or DMS secret.
			log.Error(err, "Error reading object. Requeuing request")
			// NOTE originally updated metrics here, this has been removed
			return reconcile.Result{}, nil
		}
	}

	// do the work! collect secret info for PD and DMS
	pagerdutyRoutingKey := ""
	watchdogURL := ""
	// If a secret exists, add the necessary configs to Alertmanager.
	if pagerDutySecretExists {
		log.Info("INFO: Pager Duty secret exists")
		pagerdutyRoutingKey = readSecretKey(r, &request, secretNamePD, secretKeyPD)
	}
	if snitchSecretExists {
		log.Info("INFO: Dead Man's Snitch secret exists")
		watchdogURL = readSecretKey(r, &request, secretNameDMS, secretKeyDMS)
	}

	// grab the console URL for PD alerts
	consoleUrl, err := r.getWebConsoleUrl()
	if err != nil {
		log.Error(err, "unable to determine console URL")
	}

	// create the desired alertmanager Config
	clusterID, err := r.getClusterID()
	if err != nil {
		log.Error(err, "Error reading cluster id.")
	}
	alertmanagerconfig := createAlertManagerConfig(pagerdutyRoutingKey, watchdogURL, consoleUrl, clusterID)

	// write the alertmanager Config
	writeAlertManagerConfig(r, alertmanagerconfig)
	// Update metrics after all reconcile operations are complete.
	metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
	reqLogger.Info("Finished reconcile for secret.")
	return reconcile.Result{}, nil
}

func (r *ReconcileSecret) getClusterID() (string, error) {
	var version configv1.ClusterVersion
	err := r.client.Get(context.TODO(), client.ObjectKey{Name: "version"}, &version)
	if err != nil {
		return "", err
	}
	return string(version.Spec.ClusterID), nil
}

// secretInList takes the name of Secret, and a list of Secrets, and returns a Bool
// indicating if the name is present in the list
func secretInList(name string, list *corev1.SecretList) bool {
	for _, secret := range list.Items {
		if name == secret.Name {
			log.Info("DEBUG: Secret named", secret.Name, "found")
			return true
		}
	}
	log.Info("DEBUG: Secret", name, "not found")
	return false
}

// readSecretKey fetches the data from a Secret, such as a PagerDuty API key.
func readSecretKey(r *ReconcileSecret, request *reconcile.Request, secretname string, fieldname string) string {

	secret := &corev1.Secret{}

	// Define a new objectKey for fetching the secret key.
	objectKey := client.ObjectKey{
		Namespace: request.Namespace,
		Name:      secretname,
	}

	// Fetch the key from the secret object.
	r.client.Get(context.TODO(), objectKey, secret)
	secretkey := secret.Data[fieldname]

	return string(secretkey)
}

// writeAlertManagerConfig writes the updated alertmanager config to the `alertmanager-main` secret in namespace `openshift-monitoring`.
func writeAlertManagerConfig(r *ReconcileSecret, amconfig *alertmanager.Config) {
	amconfigbyte, marshalerr := yaml.Marshal(amconfig)
	if marshalerr != nil {
		log.Error(marshalerr, "ERROR: failed to marshal Alertmanager config")
	}
	// This is commented out because it prints secrets, but it might be useful for debugging when running locally.
	//log.Info("DEBUG: Marshalled Alertmanager config:", string(amconfigbyte))

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
		log.Error(err, "ERROR: Could not write secret alertmanger-main", "namespace", secret.Namespace)
		return
	}
	log.Info("INFO: Secret alertmanager-main successfully updated")
}
