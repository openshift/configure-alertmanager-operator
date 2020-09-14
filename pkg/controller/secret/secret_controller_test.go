package secret

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/configure-alertmanager-operator/config"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	exampleConsoleUrl = "https://console-openshift-console.apps.cluster.abcd.t1.example.com"
	exampleClusterId  = "fake-cluster-id"
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
		message = fmt.Sprintf("%s: Expected '%v' but got '%v'", message, want, got)
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

func verifyInhibitRules(t *testing.T, inhibitRules []*alertmanager.InhibitRule) {
	tests := []struct {
		SourceMatch   map[string]string
		TargetMatchRE map[string]string
		Equal         []string
		Expected      bool
	}{
		{
			SourceMatch: map[string]string{
				"alertname": "NotPresent",
			},
			TargetMatchRE: map[string]string{
				"alertname": "DoesNotExist",
			},
			Equal: []string{
				"namespace",
				"missing",
			},
			Expected: false,
		},
		{
			SourceMatch: map[string]string{
				"severity": "critical",
			},
			TargetMatchRE: map[string]string{
				"severity": "warning|info",
			},
			Equal: []string{
				"namespace",
				"alertname",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"severity": "warning",
			},
			TargetMatchRE: map[string]string{
				"severity": "info",
			},
			Equal: []string{
				"namespace",
				"alertname",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "ClusterOperatorDown",
			},
			TargetMatchRE: map[string]string{
				"alertname": "ClusterOperatorDegraded",
			},
			Equal: []string{
				"namespace",
				"name",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "KubeNodeNotReady",
			},
			TargetMatchRE: map[string]string{
				"alertname": "KubeDaemonSetRolloutStuck",
			},
			Equal: []string{
				"namespace",
				"instance",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "KubePodNotReady",
			},
			TargetMatchRE: map[string]string{
				"alertname": "KubeDeploymentReplicasMismatch",
			},
			Equal: []string{
				"namespace",
				"pod",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "KubePodCrashLooping",
			},
			TargetMatchRE: map[string]string{
				"alertname": "KubeDeploymentReplicasMismatch",
			},
			Equal: []string{
				"namespace",
				"pod",
			},
			Expected: true,
		},
	}

	// keep track of which inhibition rules were affirmatively tested
	var presentInhibitionRules []int

	// confirm that the expected inhibition rules are present
	for _, test := range tests {
		present := false

		for i, inhibitRule := range inhibitRules {
			if reflect.DeepEqual(inhibitRule.SourceMatch, test.SourceMatch) && reflect.DeepEqual(inhibitRule.TargetMatchRE, test.TargetMatchRE) && reflect.DeepEqual(inhibitRule.Equal, test.Equal) {
				present = true
				presentInhibitionRules = append(presentInhibitionRules, i)
			}
		}

		assertEquals(t, present, test.Expected, fmt.Sprintf("expected: %+v", test))
	}

	// confirm that the present inhibition rules are expected
	for i := 0; i < len(inhibitRules); i++ {
		inhibitionRuleExpected := false

		for _, p := range presentInhibitionRules {
			if i == p {
				inhibitionRuleExpected = true
			}
		}

		rule, _ := json.Marshal(inhibitRules[i])
		assertTrue(t, inhibitionRuleExpected, fmt.Sprintf("Unexpected InhibitRule: %s", rule))
	}
}

func Test_createPagerdutyRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createPagerdutyRoute()

	verifyPagerdutyRoute(t, route)
}

func Test_createPagerdutyReceivers_WithoutKey(t *testing.T) {
	assertEquals(t, 0, len(createPagerdutyReceivers("", "", "")), "Number of Receivers")
}

func Test_createPagerdutyReceivers_WithKey(t *testing.T) {
	key := "abcdefg1234567890"

	receivers := createPagerdutyReceivers(key, exampleConsoleUrl, exampleClusterId)

	verifyPagerdutyReceivers(t, key, receivers)
}

func Test_createWatchdogRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createWatchdogRoute()

	verifyWatchdogRoute(t, route)
}

func Test_createWatchdogReceivers_WithoutURL(t *testing.T) {
	assertEquals(t, 0, len(createWatchdogReceivers("")), "Number of Receivers")
}

func Test_createWatchdogReceivers_WithKey(t *testing.T) {
	url := "http://whatever/something"

	receivers := createWatchdogReceivers(url)

	verifyWatchdogReceiver(t, url, receivers)
}

func Test_createAlertManagerConfig_WithoutKey_WithoutURL(t *testing.T) {
	pdKey := ""
	wdURL := ""

	config := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

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

	verifyInhibitRules(t, config.InhibitRules)
}

func Test_createAlertManagerConfig_WithKey_WithoutURL(t *testing.T) {
	pdKey := "poiuqwer78902345"
	wdURL := ""

	config := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 1, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 3, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)

	verifyPagerdutyRoute(t, config.Route.Routes[0])
	verifyPagerdutyReceivers(t, pdKey, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

func Test_createAlertManagerConfig_WithKey_WithURL(t *testing.T) {
	pdKey := "poiuqwer78902345"
	wdURL := "http://theinterwebs"

	config := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 2, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 4, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)

	verifyPagerdutyRoute(t, config.Route.Routes[0])
	verifyPagerdutyReceivers(t, pdKey, config.Receivers)

	verifyWatchdogRoute(t, config.Route.Routes[1])
	verifyWatchdogReceiver(t, wdURL, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

func Test_createAlertManagerConfig_WithoutKey_WithURL(t *testing.T) {
	pdKey := ""
	wdURL := "http://theinterwebs"

	config := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 1, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 2, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)
	verifyWatchdogRoute(t, config.Route.Routes[0])
	verifyWatchdogReceiver(t, wdURL, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

// createConsolePublicConfigMap creates a fake namespace/configmap with console details
func createConsolePublicConfigMap(reconciler *ReconcileSecret, t *testing.T) {
	err := reconciler.client.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: openShiftConfigManagedNamespaceName}})
	if err != nil {
		// exit the test if we can't create the namespace. Every test depends on this.
		t.Errorf("Couldn't create the required namespace for the test. Encountered error: %s", err)
		panic("Exiting due to fatal error")
	}
	newconfigmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consolePublicConfigMap,
			Namespace: openShiftConfigManagedNamespaceName,
		},
		Data: map[string]string{
			"consoleURL": exampleConsoleUrl,
		},
	}
	reconciler.client.Create(context.TODO(), newconfigmap)
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

// createReconciler creates a fake ReconcileSecret for testing.
func createReconciler(t *testing.T) *ReconcileSecret {
	scheme := scheme.Scheme

	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Unable to add route scheme: (%v)", err)
	}
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
	pdKey := "asdaidsgadfi9853"
	wdURL := "http://theinterwebs/asdf"

	configExpected := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	reconciler := createReconciler(t)
	createNamespace(reconciler, t)
	createConsolePublicConfigMap(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
	createClusterVersion(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, "pd-secret")
	reconciler.Reconcile(*req)

	// read config and a copy for comparison
	configActual := readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

// Test updating the config and making sure it is updated as expected
func Test_createPagerdutySecret_Update(t *testing.T) {
	pdKey := "asdaidsgadfi9853"
	wdURL := "http://theinterwebs/asdf"

	configExpected := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	reconciler := createReconciler(t)
	createNamespace(reconciler, t)
	createConsolePublicConfigMap(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createClusterVersion(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, secretNamePD)
	reconciler.Reconcile(*req)

	// verify what we have configured is NOT what we expect at the end (we have updates to do still)
	configActual := readAlertManagerConfig(reconciler, req)
	assertNotEquals(t, configExpected, configActual, "Config Deep Comparison")

	// update environment
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
	req = createReconcileRequest(reconciler, secretNameDMS)
	reconciler.Reconcile(*req)

	// read config and compare
	configActual = readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

func createClusterVersion(reconciler *ReconcileSecret) {
	clusterVersion := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Spec: configv1.ClusterVersionSpec{
			ClusterID: exampleClusterId,
		},
	}
	reconciler.client.Create(context.TODO(), clusterVersion)
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
		reconciler := createReconciler(t)
		createNamespace(reconciler, t)
		createConsolePublicConfigMap(reconciler, t)
		createClusterVersion(reconciler)

		pdKey := ""
		wdURL := ""

		// Create the secrets for this specific test.
		if tt.amExists {
			writeAlertManagerConfig(reconciler, createAlertManagerConfig("", "", "", ""))
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

		configExpected := createAlertManagerConfig(pdKey, wdURL, exampleConsoleUrl, exampleClusterId)

		verifyInhibitRules(t, configExpected.InhibitRules)

		req := createReconcileRequest(reconciler, secretNameAlertmanager)
		reconciler.Reconcile(*req)

		// load the config and check it
		configActual := readAlertManagerConfig(reconciler, req)

		// NOTE compare of the objects will fail when no secrets are created for some reason, so using .String()
		assertEquals(t, configExpected.String(), configActual.String(), tt.name)
	}
}
