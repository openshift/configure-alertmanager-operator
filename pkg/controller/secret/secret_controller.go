package secret

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"

	yaml "gopkg.in/yaml.v2"
)

var log = logf.Log.WithName("secret_controller")

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

var _ reconcile.Reconciler = &ReconcileSecret{}

// ReconcileSecret reconciles a Secret object
type ReconcileSecret struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Secret object and makes changes based on the state read.
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSecret) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Name", request.Name)
	reqLogger.Info("Reconciling Secret")

	// This operator is only interested in the 3 secrets listed below. Skip reconciling for all other secrets.
	if request.Name != "alertmanager-main" && request.Name != "dms-secret" && request.Name != "pd-secret" {
		reqLogger.Info("Skip reconcile: No changes detected to alertmanager secrets.")
		return reconcile.Result{}, nil
	}
	log.Info("DEBUG: Started reconcile loop")

	// Get a list of all Secrets in the `openshift-monitoring` namespace.
	// This is used for determining which secrets are present so that the necessary
	// Alertmanager config changes can happen later.
	secretList := &corev1.SecretList{}
	opts := client.ListOptions{Namespace: request.Namespace}
	r.client.List(context.TODO(), &opts, secretList)

	// Check for the presence of specific secrets.
	pagerDutySecretExists := secretInList("pd-secret", secretList)
	snitchSecretExists := secretInList("dms-secret", secretList)
	alertmanagerSecretExists := secretInList("alertmanager-main", secretList)

	// Extract the alertmanager config from the alertmanager-main secret.
	// If it doesn't exist yet, requeue this request and try again later.
	// Update metrics before exiting so Prometheus is aware of the missing secret.
	alertmanagerconfig := alertmanager.Config{}
	if alertmanagerSecretExists {
		alertmanagerconfig = getAlertManagerConfig(r, &request)
	} else {
		log.Info("Alertmanager secret (alertmanager-main) does not exist. Waiting for cluster-monitoring-operator to create it")
		metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
		return reconcile.Result{}, nil
	}

	// This block looks at a specific instance of Secret. This is done for each Secret
	// in the `openshift-monitoring` namespace. In the case of a deleted Secret,
	// the Alertmanager config associated with that Secret is removed.
	instance := &corev1.Secret{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("INFO: This secret has been deleted:", request.Name)
			if request.Name == "pd-secret" {
				log.Info("INFO: Pager Duty secret is absent. Removing Pager Duty config from Alertmanager")
				alertmanagerconfig := getAlertManagerConfig(r, &request)
				removeConfigFromAlertManager(r, &request, &alertmanagerconfig, "pagerduty")
				updateAlertManagerConfig(r, &request, &alertmanagerconfig)
				// Update metrics before exiting so Prometheus is aware of the missing secret.
				metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
				return reconcile.Result{}, nil
			}
			if request.Name == "dms-secret" {
				alertmanagerconfig := getAlertManagerConfig(r, &request)
				log.Info("INFO: Dead Man's Snitch secret is absent. Removing Watchdog config from Alertmanager")
				removeConfigFromAlertManager(r, &request, &alertmanagerconfig, "watchdog")
				updateAlertManagerConfig(r, &request, &alertmanagerconfig)
				// Update metrics before exiting so Prometheus is aware of the missing secret.
				metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
				return reconcile.Result{}, nil
			}
		} else {
			// Error and requeue in all other circumstances.
			// Don't requeue if a Secret is not found. It's valid to have an absent Pager Duty or DMS secret.
			log.Error(err, "Error reading object. Requeuing request")
			metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
			return reconcile.Result{}, nil
		}
	}

	// If a secret exists, add the necessary configs to Alertmanager.
	if pagerDutySecretExists {
		log.Info("INFO: Pager Duty secret exists")
		pdsecret := getSecretKey(r, &request, "pd-secret", "PAGERDUTY_KEY")
		addPDSecretToAlertManagerConfig(r, &request, &alertmanagerconfig, pdsecret)
		updateAlertManagerConfig(r, &request, &alertmanagerconfig)
	}
	if snitchSecretExists {
		log.Info("INFO: Dead Man's Snitch secret exists")
		snitchsecret := getSecretKey(r, &request, "dms-secret", "SNITCH_URL")
		addSnitchSecretToAlertManagerConfig(r, &request, &alertmanagerconfig, snitchsecret)
		updateAlertManagerConfig(r, &request, &alertmanagerconfig)
	}

	// Update metrics after all reconcile operations are complete.
	metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)

	reqLogger.Info("Finished reconcile for secret.")
	return reconcile.Result{}, nil
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

// getSecretKey fetches the data from a Secret, such as a PagerDuty API key.
func getSecretKey(r *ReconcileSecret, request *reconcile.Request, secretname string, fieldname string) string {

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

// getAlertManagerConfig fetches the AlertManager configuration from its default location.
// This is equivalent to `oc get secrets -n openshift-monitoring alertmanager-main`.
// It specifically extracts the .data "alertmanager.yaml" field, and loads it into a resource
// of type Config, enabling it to be marshalled and unmarshalled as needed.
func getAlertManagerConfig(r *ReconcileSecret, request *reconcile.Request) alertmanager.Config {

	amconfig := alertmanager.Config{}

	secret := &corev1.Secret{}

	// Define a new objectKey for fetching the alertmanager config.
	objectKey := client.ObjectKey{
		Namespace: request.Namespace,
		Name:      "alertmanager-main",
	}

	// Fetch the alertmanager config and load it into an alertmanager.Config struct.
	r.client.Get(context.TODO(), objectKey, secret)
	secretdata := secret.Data["alertmanager.yaml"]
	err := yaml.Unmarshal(secretdata, &amconfig)
	if err != nil {
		panic(err)
	}

	return amconfig
}

// addPDSecretToAlertManagerConfig adds the Pager Duty integration settings into the existing Alertmanager config.
// The changes are kept in memory until committed using function updateAlertManagerConfig().
func addPDSecretToAlertManagerConfig(r *ReconcileSecret, request *reconcile.Request, amconfig *alertmanager.Config, pdsecret string) {

	// Define the contents of the PagerDutyConfig.
	pdconfig := &alertmanager.PagerdutyConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		RoutingKey:     pdsecret,
		Severity:       `{{ if .CommonLabels.severity }}{{ .CommonLabels.severity | toLower }}{{ else }}critical{{ end }}`,
		Description:    `{{ .CommonLabels.alertname }} {{ .CommonLabels.severity | toUpper }} ({{ len .Alerts }})`,
		Details: map[string]string{
			"link":         `{{ if .CommonAnnotations.link }}{{ .CommonAnnotations.link }}{{ else }}https://github.com/openshift/ops-sop/tree/master/v4/alerts/{{ .CommonLabels.alertname }}.md{{ end }}`,
			"group":        `{{ .CommonLabels.alertname }}`,
			"component":    `{{ .CommonLabels.alertname }}`,
			"num_firing":   `{{ .Alerts.Firing | len }}`,
			"num_resolved": `{{ .Alerts.Resolved | len }}`,
			"resolved":     `{{ template "pagerduty.default.instances" .Alerts.Resolved }}`,
		},
	}

	// Overwrite the existing Pager Duty config with the updated version specified above.
	// This keeps other receivers intact while updating only the Pager Duty receiver.
	pagerdutyabsent := true
	for i, receiver := range amconfig.Receivers {
		log.Info("DEBUG: Found Receiver named:", receiver.Name)
		if receiver.Name == "pagerduty" {
			log.Info("DEBUG: Overwriting Pager Duty config for Receiver:", receiver.Name)
			amconfig.Receivers[i].PagerdutyConfigs = []*alertmanager.PagerdutyConfig{pdconfig}
			pagerdutyabsent = false
		} else {
			log.Info("DEBUG: Skipping Receiver named", receiver.Name)
		}
	}

	// Create the Pager Duty config if it doesn't already exist.
	if pagerdutyabsent {
		log.Info("Pager Duty receiver is absent. Creating new receiver.")
		newreceiver := &alertmanager.Receiver{
			Name:             "pagerduty",
			PagerdutyConfigs: []*alertmanager.PagerdutyConfig{pdconfig},
		}
		amconfig.Receivers = append(amconfig.Receivers, newreceiver)
	}

	// Create a route for the new Pager Duty receiver
	pdroute := &alertmanager.Route{
		Continue: true,
		Receiver: "pagerduty",
		GroupByStr: []string{
			"alertname",
			"severity",
		},
		// Commenting this out until we get more labels from prometheus-k8s-alert
		/*MatchRE: map[string]string{
			"namespace": alertmanager.PDRegex,
		},*/
	}

	// Insert the Route for the Pager Duty Receiver.
	routeabsent := true
	for i, route := range amconfig.Route.Routes {
		log.Info("DEBUG: Found Route for Receiver:", route.Receiver)
		if route.Receiver == "pagerduty" {
			log.Info("DEBUG: Overwriting Pager Duty Route for Receiver:", route.Receiver)
			amconfig.Route.Routes[i] = pdroute
			routeabsent = false
		} else {
			log.Info("DEBUG: Skipping Route for Receiver named", route.Receiver)
		}
	}

	// Create Route for Pager Duty Receiver if it doesn't already exist.
	if routeabsent {
		log.Info("Route for Pager Duty Receiver is absent. Creating new Route.")
		amconfig.Route.Routes = append(amconfig.Route.Routes, pdroute)
	}
}

// updateAlertManagerConfig writes the updated alertmanager config to the `alertmanager-main` secret in namespace `openshift-monitoring`.
func updateAlertManagerConfig(r *ReconcileSecret, request *reconcile.Request, amconfig *alertmanager.Config) {

	amconfigbyte, marshalerr := yaml.Marshal(amconfig)
	if marshalerr != nil {
		log.Error(marshalerr, "ERROR: failed to marshal Alertmanager config")
	}
	// This is commented out because it prints secrets, but it might be useful for debugging when running locally.
	//log.Info("DEBUG: Marshalled Alertmanager config:", string(amconfigbyte))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alertmanager-main",
			Namespace: "openshift-monitoring",
		},
		Data: map[string][]byte{
			"alertmanager.yaml": amconfigbyte,
		},
	}

	// Write the alertmanager config into the alertmanager secret.
	err := r.client.Update(context.TODO(), secret)
	if err != nil {
		log.Error(err, "ERROR: Could not write secret alertmanger-main in namespace", request.Namespace)
		return
	}
	log.Info("INFO: Secret alertmanager-main successfully updated")
}

// addSnitchSecretToAlertManagerConfig adds the Dead Man's Snitch settings into the existing Alertmanager config.
// The changes are kept in memory until committed using function updateAlertManagerConfig().
func addSnitchSecretToAlertManagerConfig(r *ReconcileSecret, request *reconcile.Request, amconfig *alertmanager.Config, snitchsecret string) {

	// Define the contents of the WebhookConfig which is part of the Watchdog receiver.
	// The Watchdog receiver uses the Dead Man's Snitch external service as its webhook.
	snitchconfig := &alertmanager.WebhookConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		URL:            snitchsecret,
	}

	// Overwrite the existing Watchdog config with the updated version specified above.
	// This keeps other receivers intact while updating only the Watchdog related receivers.
	watchdogabsent := true
	log.Info("DEBUG: Checking for watchdog related receivers")
	for i, receiver := range amconfig.Receivers {
		log.Info("DEBUG: Found Receiver named:", receiver.Name)
		switch receiver.Name {
		case "watchdog":
			log.Info("DEBUG: Overwriting watchdog receiver:", receiver.Name)
			amconfig.Receivers[i].WebhookConfigs = []*alertmanager.WebhookConfig{snitchconfig}
			watchdogabsent = false
		case "null":
			// Delete the default 'null' Receiver, because watchdog will become the new default.
			log.Info("DEBUG: Deleting receiver named:", receiver.Name)
			amconfig.Receivers = removeFromReceivers(amconfig.Receivers, i)
		default:
			log.Info("DEBUG: Skipping receiver named:", receiver.Name)
		}
	}

	// Create the Watchdog receiver if it doesn't already exist.
	if watchdogabsent {
		log.Info("DEBUG: Watchdog receiver is absent. Creating new receiver.")
		newreceiver := &alertmanager.Receiver{
			Name:           "watchdog",
			WebhookConfigs: []*alertmanager.WebhookConfig{snitchconfig},
		}
		amconfig.Receivers = append(amconfig.Receivers, newreceiver)
	}

	// Create a route for the new Watchdog receiver.
	wdroute := &alertmanager.Route{
		Receiver:       "watchdog",
		RepeatInterval: "5m",
		Match:          map[string]string{"alertname": "Watchdog"},
	}

	// Insert the Route for the Watchdog Receiver.
	routeabsent := true
	log.Info("DEBUG: Checking for watchdog related routes")
	for i, route := range amconfig.Route.Routes {
		log.Info("DEBUG: Found Route for Receiver:", route.Receiver)
		switch route.Receiver {
		case "watchdog":
			log.Info("DEBUG: Overwriting Watchdog Route for Receiver:", route.Receiver)
			amconfig.Route.Routes[i] = wdroute
			routeabsent = false
		case "null":
			// Remove null route, since the watchdog route replaces it.
			log.Info("DEBUG: Deleting Route for Receiver:", route.Receiver)
			amconfig.Route.Routes = removeFromRoutes(amconfig.Route.Routes, i)
		default:
			log.Info("DEBUG: Skipping route for receiver named:", route.Receiver)
		}
	}

	// Create Route for Watchdog Receiver if it doesn't already exist.
	if routeabsent {
		log.Info("DEBUG: Route for Watchdog Receiver is absent. Creating new Route.")
		amconfig.Route.Routes = append(amconfig.Route.Routes, wdroute)
	}

	// Update the default route to point to our new receiver.
	amconfig.Route.Receiver = "watchdog"

}

// removeFromReceivers removes the specified index from a slice of Receivers.
func removeFromReceivers(r []*alertmanager.Receiver, i int) []*alertmanager.Receiver {
	r = append(r[:i], r[i+1:]...)
	return r
}

// removeFromRoutes removes the specified index from a slice of Routes.
func removeFromRoutes(r []*alertmanager.Route, i int) []*alertmanager.Route {
	r = append(r[:i], r[i+1:]...)
	return r
}

// removeConfigFromAlertManager removes a Receiver config and the associated Route from Alertmanager.
// The changes are kept in memory until committed using function updateAlertManagerConfig().
func removeConfigFromAlertManager(r *ReconcileSecret, request *reconcile.Request, amconfig *alertmanager.Config, receivername string) {
	log.Info("DEBUG: Checking for receiver", receivername, "in Alertmanager config")
	for i, receiver := range amconfig.Receivers {
		if receiver.Name == receivername {
			log.Info("DEBUG: Deleting receiver named:", receiver.Name)
			amconfig.Receivers = removeFromReceivers(amconfig.Receivers, i)
		}
	}
	for i, route := range amconfig.Route.Routes {
		if route.Receiver == receivername {
			log.Info("DEBUG: Deleting Route for Receiver:", route.Receiver)
			amconfig.Route.Routes = removeFromRoutes(amconfig.Route.Routes, i)
		}
	}

	// If watchdog is being removed, put the system default route and receiver back into place.
	if receivername == "watchdog" {
		amconfig.Route.Receiver = "null"
		nullroute := &alertmanager.Route{
			Receiver: "null",
			Match:    map[string]string{"alertname": "Watchdog"},
		}
		amconfig.Route.Routes = append(amconfig.Route.Routes, nullroute)
		nullreceiver := &alertmanager.Receiver{Name: "null"}
		amconfig.Receivers = append(amconfig.Receivers, nullreceiver)
	}
}
