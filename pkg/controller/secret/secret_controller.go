package secret

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"

	amcv1alpha1 "github.com/openshift/configure-alertmanager-operator/pkg/apis/alertmanager/v1alpha1"
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

	// Watch for changes to secondary resource AlertManagerConfiguration,
	// and enqueue a request for the Alertmanager secret
	err = c.Watch(
		&source.Kind{Type: &amcv1alpha1.AlertManagerConfiguration{}},
		handler.Funcs{
			CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      secretNameAlertmanager,
					Namespace: e.Meta.GetNamespace(),
				}})
			},
			UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      secretNameAlertmanager,
					Namespace: e.MetaNew.GetNamespace(),
				}})
			},
			DeleteFunc: func(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      secretNameAlertmanager,
					Namespace: e.Meta.GetNamespace(),
				}})
			},
			GenericFunc: func(e event.GenericEvent, q workqueue.RateLimitingInterface) {
				q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      secretNameAlertmanager,
					Namespace: e.Meta.GetNamespace(),
				}})
			},
		},
	)
	if err != nil {
		return err
	}

	return nil
}

// createAlertManagerConfig creates an AlertManager Config in memory based on the provided input parameters.
func createAlertManagerConfig(
	routes []*alertmanager.Route,
	receivers []*alertmanager.Receiver,
	inhibitRules []*alertmanager.InhibitRule,
) *alertmanager.Config {

	if routes != nil {
		filteredRoutes := []*alertmanager.Route{}
		for _, r := range routes {
			if r != nil {
				filteredRoutes = append(filteredRoutes, r)
			}
		}
		routes = filteredRoutes

		if len(routes) == 0 {
			routes = nil
		}
	}

	if inhibitRules != nil {
		filteredInhibitRules := []*alertmanager.InhibitRule{}
		for _, i := range inhibitRules {
			if i != nil {
				filteredInhibitRules = append(filteredInhibitRules, i)
			}
		}
		inhibitRules = filteredInhibitRules

		if len(inhibitRules) == 0 {
			inhibitRules = nil
		}
	}

	if receivers == nil {
		receivers = []*alertmanager.Receiver{}
	}

	filteredReceivers := []*alertmanager.Receiver{}
	for _, r := range receivers {
		if r != nil {
			filteredReceivers = append(filteredReceivers, r)
		}
	}
	receivers = filteredReceivers

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
		Receivers:    receivers,
		Templates:    []string{},
		InhibitRules: inhibitRules,
	}

	return amconfig
}

// Reconcile reads that state of the cluster for a Secret object and makes changes based on the state read.
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileSecret) Reconcile(request reconcile.Request) (reconcile.Result, error) {
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

	routes, receivers, inhibitRules, err := r.handleAlertManagerConfigurationCRs()
	if err != nil {
		return reconcile.Result{}, err
	}

	// create the desired alertmanager Config
	alertmanagerconfig := createAlertManagerConfig(routes, receivers, inhibitRules)

	// write the alertmanager Config
	writeAlertManagerConfig(r, alertmanagerconfig)
	// Update metrics after all reconcile operations are complete.
	metrics.UpdateSecretsMetrics(secretList, alertmanagerconfig)
	reqLogger.Info("Finished reconcile for secret.")
	return reconcile.Result{}, nil
}

func (r *ReconcileSecret) handleAlertManagerConfigurationCRs() (
	[]*alertmanager.Route,
	[]*alertmanager.Receiver,
	[]*alertmanager.InhibitRule,
	error,
) {
	routes := []*alertmanager.Route{}
	receivers := []*alertmanager.Receiver{}
	inhibitRules := []*alertmanager.InhibitRule{}

	amConfigList := &amcv1alpha1.AlertManagerConfigurationList{}
	err := r.client.List(context.TODO(), amConfigList)
	if err != nil {
		log.Error(err, "Error listing AlertManagerConfiguration CRs")
		return routes, receivers, inhibitRules, err
	}

	for _, amc := range amConfigList.Items {
		if !reflect.DeepEqual(amc.Spec.Route, amcv1alpha1.Route{}) {
			routes = append(routes, amc.Spec.Route.ToAMRoute(amc.ObjectMeta))
		}

		for _, rec := range amc.Spec.Receivers {
			receivers = append(receivers, rec.ToAMReceiver(amc.ObjectMeta, readSecretKeySelector(r.client)))
		}

		for _, ir := range amc.Spec.InhibitRules {
			inhibitRules = append(inhibitRules, ir.ToAMInhibitRule())
		}
	}
	return routes, receivers, inhibitRules, nil
}

func readSecretKeySelector(k8sClient client.Client) func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error) {
	return func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error) {
		secret := &corev1.Secret{}
		err := k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: secretKeySelector.Name}, secret)
		if err != nil {
			return "", fmt.Errorf("Error getting secret: %w", err)
		}

		if val, ok := secret.Data[secretKeySelector.Key]; ok {
			return string(val), nil
		}
		return "", fmt.Errorf("Key %v not found in secret %v", secretKeySelector.Key, secretKeySelector.Name)
	}
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
