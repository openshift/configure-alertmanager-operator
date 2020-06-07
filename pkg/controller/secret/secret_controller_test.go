package secret

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/openshift/configure-alertmanager-operator/config"
	"github.com/openshift/configure-alertmanager-operator/pkg/apis"
	amcv1alpha1 "github.com/openshift/configure-alertmanager-operator/pkg/apis/alertmanager/v1alpha1"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// readAlertManagerConfig fetches the AlertManager configuration from its default location.
// This is equivalent to `oc get secrets -n openshift-monitoring alertmanager-main`.
// It specifically extracts the .data "alertmanager.yaml" field, and loads it into a resource
// of type Config, enabling it to be marshalled and unmarshalled as needed.
func readAlertManagerConfig(r *ReconcileSecret, request *reconcile.Request) *alertmanager.Config {
	amconfig := &alertmanager.Config{}

	secret := &corev1.Secret{}

	// Define a new objectKey for fetching the alertmanager config.
	objectKey := client.ObjectKey{
		Namespace: request.Namespace,
		Name:      secretNameAlertmanager,
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

func assertEquals(t *testing.T, want interface{}, got interface{}, message string) {
	if reflect.DeepEqual(got, want) {
		return
	}

	if len(message) == 0 {
		message = fmt.Sprintf("Expected '%v' but got '%v'", want, got)
	} else {
		message = fmt.Sprintf("%s: Expected '%v' but got '%v'", message, want, got)
	}
	t.Fatal(message)
}

func assertNotEquals(t *testing.T, want interface{}, got interface{}, message string) {
	if !reflect.DeepEqual(got, want) {
		return
	}
	if len(message) == 0 {
		message = fmt.Sprintf("Didn't expect '%v'", want)
	} else {
		message = fmt.Sprintf("%s: Didn't expect '%v'", message, want)
	}
	t.Fatal(message)
}

func assertGte(t *testing.T, want int, got int, message string) {
	if want <= got {
		return
	}
	if len(message) == 0 {
		message = fmt.Sprintf("Expected '%v' but got '%v'", want, got)
	} else {
		message = fmt.Sprintf("%s: Expected '%v' but got '%v'", message, want, got)
	}
	t.Fatal(message)
}

func assertTrue(t *testing.T, status bool, message string) {
	if status {
		return
	}
	t.Fatal(message)
}

// utility class to test PD route creation
func verifyPagerdutyRoute(t *testing.T, route *alertmanager.Route) {
	assertEquals(t, defaultReceiver, route.Receiver, "Receiver Name")
	assertEquals(t, true, route.Continue, "Continue")
	assertEquals(t, []string{"alertname", "severity"}, route.GroupByStr, "GroupByStr")
	assertGte(t, 1, len(route.Routes), "Number of Routes")

	// verify we have the core routes for namespace, ES, and fluentd
	hasNamespace := false
	hasElasticsearch := false
	hasFluentd := false
	for _, route := range route.Routes {
		if route.MatchRE["namespace"] == alertmanager.PDRegex {
			hasNamespace = true
		} else if route.Match["job"] == "fluentd" {
			hasFluentd = true
		} else if route.Match["cluster"] == "elasticsearch" {
			hasElasticsearch = true
		}
	}

	assertTrue(t, hasNamespace, "No route for MatchRE on namespace")
	assertTrue(t, hasElasticsearch, "No route for Match on cluster=elasticsearch")
	assertTrue(t, hasFluentd, "No route for Match on job=fluentd")
}

func verifyNullReceiver(t *testing.T, receivers []*alertmanager.Receiver) {
	hasNull := false
	for _, receiver := range receivers {
		if receiver.Name == receiverNull {
			hasNull = true
			assertEquals(t, 0, len(receiver.PagerdutyConfigs), "Empty PagerdutyConfigs")
		}
	}
	assertTrue(t, hasNull, fmt.Sprintf("No '%s' receiver", receiverNull))
}

// utility function to verify Pagerduty Receivers
func verifyPagerdutyReceivers(t *testing.T, key string, receivers []*alertmanager.Receiver) {
	// there are at least 3 receivers: namespace, elasticsearch, and fluentd
	assertGte(t, 2, len(receivers), "Number of Receivers")

	// verify structure of each
	hasMakeItWarning := false
	hasPagerduty := false
	for _, receiver := range receivers {
		switch receiver.Name {
		case receiverMakeItWarning:
			hasMakeItWarning = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertEquals(t, "warning", receiver.PagerdutyConfigs[0].Severity, "Severity")
		case receiverPagerduty:
			hasPagerduty = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertTrue(t, receiver.PagerdutyConfigs[0].Severity != "", "Non empty Severity")
			assertNotEquals(t, "warning", receiver.PagerdutyConfigs[0].Severity, "Severity")
		}
	}

	assertTrue(t, hasMakeItWarning, fmt.Sprintf("No '%s' receiver", receiverMakeItWarning))
	assertTrue(t, hasPagerduty, fmt.Sprintf("No '%s' receiver", receiverPagerduty))
}

// utility function to verify watchdog route
func verifyWatchdogRoute(t *testing.T, route *alertmanager.Route) {
	assertEquals(t, receiverWatchdog, route.Receiver, "Receiver Name")
	assertEquals(t, "5m", route.RepeatInterval, "Repeat Interval")
	assertEquals(t, "Watchdog", route.Match["alertname"], "Alert Name")
}

// utility to test watchdog receivers
func verifyWatchdogReceiver(t *testing.T, url string, receivers []*alertmanager.Receiver) {
	// there is 1 receiver
	assertGte(t, 1, len(receivers), "Number of Receivers")

	// verify structure of each
	hasWatchdog := false
	for _, receiver := range receivers {
		if receiver.Name == receiverWatchdog {
			hasWatchdog = true
			assertTrue(t, receiver.WebhookConfigs[0].VSendResolved, "VSendResolved")
			assertEquals(t, url, receiver.WebhookConfigs[0].URL, "URL")
		}
	}

	assertTrue(t, hasWatchdog, fmt.Sprintf("No '%s' receiver", receiverWatchdog))
}

func Test_createAlertManagerConfig_With_Empty_Input(t *testing.T) {
	routes := []*alertmanager.Route{}
	receivers := []*alertmanager.Receiver{}
	inhibitRules := []*alertmanager.InhibitRule{}

	config := createAlertManagerConfig(routes, receivers, inhibitRules)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 0, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 1, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)
}

func Test_createAlertManagerConfig_With_nil_slices(t *testing.T) {
	var (
		routes       []*alertmanager.Route       = nil
		receivers    []*alertmanager.Receiver    = nil
		inhibitRules []*alertmanager.InhibitRule = nil
	)

	config := createAlertManagerConfig(routes, receivers, inhibitRules)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 0, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 1, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)
}

func Test_createAlertManagerConfig_With_nil_elements(t *testing.T) {
	routes := []*alertmanager.Route{nil}
	receivers := []*alertmanager.Receiver{nil}
	inhibitRules := []*alertmanager.InhibitRule{nil}

	config := createAlertManagerConfig(routes, receivers, inhibitRules)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 0, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 1, len(config.Receivers), "Receivers")
}

// createSecret creates a fake Secret to use in testing.
func createSecret(reconciler *ReconcileSecret, secretname string, secretkey string, secretdata string) {
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

func createAlertManagerConfiguration(
	reconciler *ReconcileSecret,
	route amcv1alpha1.Route,
	receivers []amcv1alpha1.Receiver,
	inhibitRules []amcv1alpha1.InhibitRule,
) {
	amc := &amcv1alpha1.AlertManagerConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "dummy", Namespace: config.OperatorNamespace},
		Spec: amcv1alpha1.AlertManagerConfigurationSpec{
			Route:        route,
			Receivers:    receivers,
			InhibitRules: inhibitRules,
		},
	}
	reconciler.client.Create(context.TODO(), amc)
}

// createReconciler creates a fake ReconcileSecret for testing.
func createReconciler() *ReconcileSecret {
	scheme := runtime.NewScheme()
	corev1.SchemeBuilder.AddToScheme(scheme)
	apis.AddToScheme(scheme)
	monitoringv1.AddToScheme(scheme)

	return &ReconcileSecret{
		client: fake.NewFakeClientWithScheme(scheme),
		scheme: scheme,
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

// Test_updateAlertManagerConfig tests writing to the Alertmanager config.
func Test_createPagerdutySecret_Create(t *testing.T) {
	routes := []*alertmanager.Route{}
	receivers := []*alertmanager.Receiver{}
	var inhibitRules []*alertmanager.InhibitRule
	pdKey := "asdaidsgadfi9853"
	wdURL := "http://theinterwebs/asdf"

	configExpected := createAlertManagerConfig(routes, receivers, inhibitRules)

	// prepare environment
	reconciler := createReconciler()
	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, "pd-secret")
	reconciler.Reconcile(*req)

	// read config and a copy for comparison
	configActual := readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

// Test updating the config and making sure it is updated as expected
func Test_createPagerdutySecret_Update(t *testing.T) {
	routes := []*alertmanager.Route{{
		Receiver: "openshift-monitoring-dummy-dummy-receiver",
		Continue: true,
	}}
	receivers := []*alertmanager.Receiver{}
	var inhibitRules []*alertmanager.InhibitRule

	configExpected := createAlertManagerConfig(routes, receivers, inhibitRules)

	// prepare environment
	reconciler := createReconciler()
	createNamespace(reconciler, t)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, secretNamePD)
	reconciler.Reconcile(*req)

	// verify what we have configured is NOT what we expect at the end (we have updates to do still)
	configActual := readAlertManagerConfig(reconciler, req)
	assertNotEquals(t, configExpected, configActual, "Config Deep Comparison")

	// update environment
	createAlertManagerConfiguration(reconciler, amcv1alpha1.Route{Receiver: "dummy-receiver"}, nil, nil)
	req = createReconcileRequest(reconciler, secretNameDMS)
	reconciler.Reconcile(*req)

	// read config and compare
	configActual = readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

func Test_ReconcileSecrets(t *testing.T) {
	type fields struct {
		client client.Client
		scheme *runtime.Scheme
	}
	tests := []struct {
		name        string
		dmsExists   bool
		pdExists    bool
		amExists    bool
		otherExists bool
	}{
		{
			name:        "Test reconcile with NO secrets.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with dms-secret only.",
			dmsExists:   true,
			pdExists:    false,
			amExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd-secret only.",
			dmsExists:   false,
			pdExists:    true,
			amExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with alertmanager-main only.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    true,
			otherExists: false,
		},
		{
			name:        "Test reconcile with 'other' secret only.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    false,
			otherExists: true,
		},
		{
			name:        "Test reconcile with pd & dms secrets.",
			dmsExists:   true,
			pdExists:    true,
			amExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd & am secrets.",
			dmsExists:   false,
			pdExists:    true,
			amExists:    true,
			otherExists: false,
		},
		{
			name:        "Test reconcile with am & dms secrets.",
			dmsExists:   true,
			pdExists:    false,
			amExists:    true,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd, dms, and am secrets.",
			dmsExists:   true,
			pdExists:    true,
			amExists:    true,
			otherExists: false,
		},
	}
	for _, tt := range tests {
		reconciler := createReconciler()
		createNamespace(reconciler, t)

		routes := []*alertmanager.Route{}
		receivers := []*alertmanager.Receiver{}
		var inhibitRules []*alertmanager.InhibitRule
		pdKey := ""
		wdURL := ""

		// Create the secrets for this specific test.
		if tt.amExists {
			writeAlertManagerConfig(reconciler, createAlertManagerConfig(routes, receivers, inhibitRules))
		}
		if tt.dmsExists {
			wdURL = "https://hjklasdf09876"
			createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
		}
		if tt.otherExists {
			createSecret(reconciler, "other", "key", "asdfjkl")
		}
		if tt.pdExists {
			pdKey = "asdfjkl123"
			createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
		}

		configExpected := createAlertManagerConfig(routes, receivers, inhibitRules)

		req := createReconcileRequest(reconciler, secretNameAlertmanager)
		reconciler.Reconcile(*req)

		// load the config and check it
		configActual := readAlertManagerConfig(reconciler, req)

		// NOTE compare of the objects will fail when no secrets are created for some reason, so using .String()
		assertEquals(t, configExpected.String(), configActual.String(), tt.name)
	}
}
