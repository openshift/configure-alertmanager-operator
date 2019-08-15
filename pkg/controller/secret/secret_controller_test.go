package secret

import (
	"context"
	"reflect"
	"testing"

	"github.com/openshift/configure-alertmanager-operator/config"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// AlertManager base config, used by multiple tests.
const AlertManagerConfig = `
global:
  resolve_timeout: 5m
route:
  receiver: "null"
  group_by:
  - job
  routes:
  - receiver: "null"
    match:
      alertname: Watchdog
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: "null"
templates: []
`

// createAlertManagerConfig creates a fake secret containing a basic AlertManager config for testing.
func createAlertManagerConfig(amconfig []byte, reconciler *ReconcileSecret) {
	amsecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alertmanager-main",
			Namespace: config.OperatorNamespace,
		},
		Data: map[string][]byte{
			"alertmanager.yaml": []byte(amconfig),
		},
	}
	reconciler.client.Create(context.TODO(), amsecret)
}

// createSecret creates a fake Secret to use in testing.
func createSecret(secretname string, secretkey string, secretdata string, reconciler *ReconcileSecret) {
	newsecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretname,
			Namespace: config.OperatorNamespace,
		},
		Data: map[string][]byte{
			secretkey: []byte(secretdata),
		},
	}
	reconciler.client.Create(context.TODO(), newsecret)
}

// createReconciler creates a fake ReconcileSecret for testing.
func createReconciler() *ReconcileSecret {
	return &ReconcileSecret{
		client: fake.NewFakeClient(),
		scheme: nil,
	}
}

// createNamespace creates a fake `openshift-monitoring` namespace for testing.
func createNamespace(reconciler *ReconcileSecret, t *testing.T) {
	err := reconciler.client.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: config.OperatorNamespace}})
	if err != nil {
		// exit the test if we can't create the namespace. Every test depends on this.
		t.Errorf("Couldn't create the required namespace for the test. Encountered error: %s", err)
		panic("Exiting due to fatal error")
	}
}

// Create the reconcile request for the specified secret.
func createReconcileRequest(reconciler *ReconcileSecret, secretname string) *reconcile.Request {
	return &reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      secretname,
			Namespace: config.OperatorNamespace,
		},
	}
}

// Test_getAlertManagerConfig ensures that the default AlertManagerConfig is the same after marshalling and unmarshalling.
// If this test fails, all other tests will fail, because it's crucial to be able to retrieve
// the alertmanager config when testing functions that read and write the alertmanager config.
func Test_getAlertManagerConfig(t *testing.T) {

	// This stores the Config object to test against.
	// It should be equal to the object created by getAlertManagerConfig.
	want := alertmanager.Config{}

	// Mock cluster data, including alertmanager-main secret, namespace, and reconcile request.

	amconfigbyte := []byte(AlertManagerConfig)
	reconciler := createReconciler()
	createNamespace(reconciler, t)
	createAlertManagerConfig(amconfigbyte, reconciler)
	req := createReconcileRequest(reconciler, "alertmanager-main")

	// Marshal the []byte into an alertmanager.Config object.
	err := yaml.Unmarshal(amconfigbyte, &want)
	if err != nil {
		t.Errorf("Unable to Unmarshal []byte into alertmanager.Config type")
		panic("Unable to continue test due to unmarshal error.")
	}

	if got := getAlertManagerConfig(reconciler, req); !reflect.DeepEqual(got, want) {
		t.Errorf("Failed because getAlertManagerConfig() returned:\n%s\n getAlertManagerConfig() should have returned:\n%s\n", got, want)
	} else {
		t.Logf("Passed, using default config. getAlertManagerConfig() returned:\n%s\n", got)
	}
}

// Test_updateAlertManagerConfig tests writing to the Alertmanager config.
func Test_updateAlertManagerConfig(t *testing.T) {

	// Load the default AlertManager config into the fake cluster.
	amconfigbyte := []byte(AlertManagerConfig)
	reconciler := createReconciler()
	createNamespace(reconciler, t)
	createAlertManagerConfig(amconfigbyte, reconciler)
	req := createReconcileRequest(reconciler, "alertmanager-main")
	amconfig := getAlertManagerConfig(reconciler, req)
	// This needs to be a copy, or else it just points to amconfig
	defaultconfig := getAlertManagerConfig(reconciler, req)

	// Update the Alertmanager config.
	amconfig.Route.GroupWait = "10s"
	updateAlertManagerConfig(reconciler, req, &amconfig)

	// Test that the change is present.
	if got := getAlertManagerConfig(reconciler, req); reflect.DeepEqual(got, defaultconfig) {
		t.Errorf("Failed because getAlertManagerConfig() shows equal results before and after writing the config")
		t.Errorf("getAlertManagerConfig() before the write:\n%s\ngetAlertManagerConfig() after the write:\n%s\n", defaultconfig, got)
		t.Errorf("Field 'group_wait' was not updated as expected")
	} else {
		t.Logf("Passed, modified 'group_wait' in default config. getAlertManagerConfig() returned:\n%s\n", got)
	}
}

// Test_addPDSecretToAlertManagerConfig tests the integration
// of 3 functions that combine to update the Pager Duty config in Alertmanager:
// getAlertManagerConfig, updateAlertManagerConfig, and addPDSecretToAlertManagerConfig.
func Test_addPDSecretToAlertManagerConfig(t *testing.T) {
	pdsecret := "asdf1234567890"

	// Load the default AlertManager config into the fake cluster.
	amconfigbyte := []byte(AlertManagerConfig)
	reconciler := createReconciler()
	createNamespace(reconciler, t)
	createSecret("pd-secret", "PAGERDUTY_KEY", pdsecret, reconciler)
	createAlertManagerConfig(amconfigbyte, reconciler)
	req := createReconcileRequest(reconciler, "pd-secret")
	want := getAlertManagerConfig(reconciler, req)

	// Add the Pager Duty details to the fake alertmanager config.
	pdconfig := &alertmanager.PagerdutyConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		RoutingKey:     pdsecret,
		Severity:       `{{ if .CommonLabels.severity }}{{ .CommonLabels.severity | toLower }}{{ else }}critical{{ end }}`,
		Description:    `{{ .CommonLabels.alertname }} {{ .CommonLabels.severity | toUpper }} ({{ len .Alerts }})`,
		Details: map[string]string{
			"link":         `{{ if .CommonAnnotations.link }}{{ .CommonAnnotations.link }}{{ else }}https://github.com/openshift/ops-sop/tree/master/v4/alerts/{{ .CommonLabels.alertname }}.md{{ end }}`,
			"link2":        `{{ if .CommonAnnotations.runbook }}{{ .CommonAnnotations.runbook }}{{ else }}{{ end }}`,
			"group":        `{{ .CommonLabels.alertname }}`,
			"component":    `{{ .CommonLabels.alertname }}`,
			"num_firing":   `{{ .Alerts.Firing | len }}`,
			"num_resolved": `{{ .Alerts.Resolved | len }}`,
			"resolved":     `{{ template "pagerduty.default.instances" .Alerts.Resolved }}`,
		},
	}
	pdroute := &alertmanager.Route{
		Continue: true,
		Receiver: "pagerduty",
		GroupByStr: []string{
			"alertname",
			"severity",
		},
		MatchRE: map[string]string{
			"namespace": alertmanager.PDRegex,
		},
	}
	pdreceiver := &alertmanager.Receiver{
		Name:             "pagerduty",
		PagerdutyConfigs: []*alertmanager.PagerdutyConfig{pdconfig},
	}
	want.Receivers = append(want.Receivers, pdreceiver)
	want.Route.Routes = append(want.Route.Routes, pdroute)
	want.Global.PagerdutyURL = "https://events.pagerduty.com/v2/enqueue"

	// Try to get the same result as `want`,
	// using addPDSecretToAlertManagerConfig() and updateAlertManagerConfig().
	defaultconfig := getAlertManagerConfig(reconciler, req)
	addPDSecretToAlertManagerConfig(reconciler, req, &defaultconfig, pdsecret)
	updateAlertManagerConfig(reconciler, req, &defaultconfig)

	// Test that the alertmanager secret now contains all the Pager Duty details,
	// as specified in var `want`.
	if got := getAlertManagerConfig(reconciler, req); !reflect.DeepEqual(got, want) {
		t.Errorf("Failed because getAlertManagerConfig() returned:\n%s\n getAlertManagerConfig() should have returned:\n%s\n", got, want)
	} else {
		t.Logf("Passed, using Pager Duty config. getAlertManagerConfig() returned:\n%s\n", got)
	}
}

// Test_addSnitchSecretToAlertManagerConfig tests adding the DMS secret
// to the Alertmanager config. It also tests functions:
// removeFromRoutes() and removeFromReceivers()
func Test_addSnitchSecretToAlertManagerConfig(t *testing.T) {
	// Set up fake cluster resources.
	snitchsecret := "https://hjkl0987654"
	reconciler := createReconciler()
	createNamespace(reconciler, t)
	createSecret("dms-secret", "SNITCH_URL", snitchsecret, reconciler)
	req := createReconcileRequest(reconciler, "dms-secret")

	// Load the default AlertManager config into the fake cluster.
	amconfigbyte := []byte(AlertManagerConfig)
	createAlertManagerConfig(amconfigbyte, reconciler)

	// Build a representation of the object we want.
	want := getAlertManagerConfig(reconciler, req)
	snitchconfig := &alertmanager.WebhookConfig{
		NotifierConfig: alertmanager.NotifierConfig{VSendResolved: true},
		URL:            snitchsecret,
	}

	// Remove default "null" receiver and replace it.
	want.Receivers = removeFromReceivers(want.Receivers, 0)
	newreceiver := &alertmanager.Receiver{
		Name:           "watchdog",
		WebhookConfigs: []*alertmanager.WebhookConfig{snitchconfig},
	}
	want.Receivers = append(want.Receivers, newreceiver)

	// Create a route for the new Watchdog receiver.
	wdroute := &alertmanager.Route{
		Receiver:       "watchdog",
		RepeatInterval: "5m",
		Match:          map[string]string{"alertname": "Watchdog"},
	}
	want.Route.Routes = removeFromRoutes(want.Route.Routes, 0)
	want.Route.Routes = append(want.Route.Routes, wdroute)
	want.Route.Receiver = "watchdog"

	// Try to get the same result as `want`,
	// using addSnitchSecretToAlertManagerConfig() and updateAlertManagerConfig().
	defaultconfig := getAlertManagerConfig(reconciler, req)
	addSnitchSecretToAlertManagerConfig(reconciler, req, &defaultconfig, snitchsecret)
	updateAlertManagerConfig(reconciler, req, &defaultconfig)

	// Test that the alertmanager secret now contains all the Dead Man's Snitch details,
	// as specified in var `want`.
	if got := getAlertManagerConfig(reconciler, req); !reflect.DeepEqual(got, want) {
		t.Errorf("Failed because getAlertManagerConfig() returned:\n%s\n getAlertManagerConfig() should have returned:\n%s\n", got, want)
	} else {
		t.Logf("Passed, using Pager Duty config. getAlertManagerConfig() returned:\n%s\n", got)
	}
}

func TestReconcileSecret_Reconcile(t *testing.T) {
	type fields struct {
		client client.Client
		scheme *runtime.Scheme
	}
	tests := []struct {
		name    string
		secret  string
		exists  bool // Indicates if the secret being reconciled exists.
		want    reconcile.Result
		wantErr bool
	}{
		{
			name:    "Test reconcile with dms-secret present.",
			secret:  "dms-secret",
			exists:  true,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with dms-secret missing.",
			secret:  "dms-secret",
			exists:  false,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with pd-secret present.",
			secret:  "pd-secret",
			exists:  true,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with pd-secret missing.",
			secret:  "pd-secret",
			exists:  false,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with alertmanager-main present.",
			secret:  "alertmanager-main",
			exists:  true,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with alertmanager-main missing.",
			secret:  "alertmanager-main",
			exists:  false,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
		{
			name:    "Test reconcile with unreconcilable secret.",
			secret:  "testsecret",
			exists:  true,
			want:    reconcile.Result{Requeue: false},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		reconciler := createReconciler()
		createNamespace(reconciler, t)

		// Create the secret for this specific test.
		if tt.exists {
			switch tt.secret {
			case "pd-secret":
				createSecret("pd-secret", "PAGERDUTY_KEY", "asdfjkl123", reconciler)
			case "dms-secret":
				createSecret("dms-secret", "SNITCH_URL", "https://hjklasdf09876", reconciler)
			case "alertmanager-main":
				amconfigbyte := []byte(AlertManagerConfig)
				createAlertManagerConfig(amconfigbyte, reconciler)
			default:
				createSecret(tt.secret, "key", "asdfjkl", reconciler)
			}
		}

		req := createReconcileRequest(reconciler, "dms-secret")

		// Load the default AlertManager config into the fake cluster.
		if tt.secret != "alertmanager-main" {
			amconfigbyte := []byte(AlertManagerConfig)
			createAlertManagerConfig(amconfigbyte, reconciler)
		}

		t.Run(tt.name, func(t *testing.T) {
			r := &ReconcileSecret{
				client: fake.NewFakeClient(),
				scheme: nil,
			}
			got, err := r.Reconcile(*req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReconcileSecret.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReconcileSecret.Reconcile() = %v, want %v", got, tt.want)
			}
		})
	}
}
