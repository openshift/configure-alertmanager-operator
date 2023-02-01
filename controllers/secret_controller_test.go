package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/configure-alertmanager-operator/config"
	"github.com/openshift/configure-alertmanager-operator/pkg/readiness"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	exampleClusterId = "fake-cluster-id"
	exampleProxy     = "https://fakeproxy.here"
)

var reqLogger = logf.Log.WithName("secret_controller")

var exampleManagedNamespaces = []string{
	"dedicated-admin",
	"openshift-aqua",
	"openshift-backplane",
	"openshift-backplane-cee",
	"openshift-backplane-managed-scripts",
}

var exampleOCPNamespaces = []string{
	"openshift-apiserver-operator",
	"openshift-authentication-operator",
	"openshift-cloud-controller-manager",
	"openshift-cloud-controller-manager-operator",
	"openshift-cloud-credential-operator",
}

// readAlertManagerConfig fetches the AlertManager configuration from its default location.
// This is equivalent to `oc get secrets -n openshift-monitoring alertmanager-main`.
// It specifically extracts the .data "alertmanager.yaml" field, and loads it into a resource
// of type Config, enabling it to be marshalled and unmarshalled as needed.
func readAlertManagerConfig(r *SecretReconciler, request *reconcile.Request) *alertmanager.Config {
	amconfig := &alertmanager.Config{}

	secret := &corev1.Secret{}

	// Define a new objectKey for fetching the alertmanager config.
	objectKey := client.ObjectKey{
		Namespace: request.Namespace,
		Name:      secretNameAlertmanager,
	}

	// Fetch the alertmanager config and load it into an alertmanager.Config struct.
	if err := r.Client.Get(context.TODO(), objectKey, secret); err != nil {
		panic(err)
	}
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

func assertFalse(t *testing.T, status bool, message string) {
	if !status {
		return
	}
	t.Fatal(message)
}

// utility class to test PD route creation
func verifyPagerdutyRoute(t *testing.T, route *alertmanager.Route, expectedNamespaces []string) {
	assertEquals(t, defaultReceiver, route.Receiver, "Receiver Name")
	assertEquals(t, true, route.Continue, "Continue")
	assertEquals(t, []string{"alertname", "severity"}, route.GroupByStr, "GroupByStr")
	assertGte(t, 1, len(route.Routes), "Number of Routes")

	// verify we have the core routes for namespace, ES, and fluentd
	hasNamespace := false
	hasElasticsearch := false
	hasFluentd := false
	routeNamespaces := []string{}
	for _, route := range route.Routes {
		if route.Receiver == receiverPagerduty && route.MatchRE["namespace"] != "" {
			routeNamespaces = append(routeNamespaces, route.MatchRE["namespace"])
		} else if route.Match["job"] == "fluentd" {
			hasFluentd = true
		} else if route.Match["cluster"] == "elasticsearch" {
			hasElasticsearch = true
		}
	}

	if reflect.DeepEqual(expectedNamespaces, routeNamespaces) {
		hasNamespace = true
	}

	assertTrue(t, hasNamespace, "No route for MatchRE on namespace")
	assertTrue(t, hasElasticsearch, "No route for Match on cluster=elasticsearch")
	assertTrue(t, hasFluentd, "No route for Match on job=fluentd")
}

// utility class to test Goalert route creation
func verifyGoalertRoute(t *testing.T, route *alertmanager.Route, expectedNamespaces []string) {
	assertEquals(t, defaultReceiver, route.Receiver, "Receiver Name")
	assertEquals(t, true, route.Continue, "Continue")
	assertEquals(t, []string{"alertname", "severity"}, route.GroupByStr, "GroupByStr")
	assertGte(t, 1, len(route.Routes), "Number of Routes")

	// verify we have the core routes for namespace, ES, and fluentd
	hasNamespace := false
	hasElasticsearch := false
	hasFluentd := false
	routeNamespaces := []string{}
	for _, route := range route.Routes {
		if route.Receiver == receiverPagerduty && route.MatchRE["namespace"] != "" {
			routeNamespaces = append(routeNamespaces, route.MatchRE["namespace"])
		} else if route.Match["job"] == "fluentd" {
			hasFluentd = true
		} else if route.Match["cluster"] == "elasticsearch" {
			hasElasticsearch = true
		}
	}

	if reflect.DeepEqual(expectedNamespaces, routeNamespaces) {
		hasNamespace = true
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
func verifyPagerdutyReceivers(t *testing.T, key string, proxy string, receivers []*alertmanager.Receiver) {
	// there are at least 3 receivers: namespace, elasticsearch, and fluentd
	assertGte(t, 2, len(receivers), "Number of Receivers")

	// verify structure of each
	hasMakeItWarning := false
	hasPagerduty := false
	hasMakeItError := false
	hasMakeItCritical := false
	for _, receiver := range receivers {
		switch receiver.Name {
		case receiverMakeItWarning:
			hasMakeItWarning = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertEquals(t, "warning", receiver.PagerdutyConfigs[0].Severity, "Severity")
			assertEquals(t, proxy, receiver.PagerdutyConfigs[0].HttpConfig.ProxyURL, "Proxy")
		case receiverPagerduty:
			hasPagerduty = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertTrue(t, receiver.PagerdutyConfigs[0].Severity != "", "Non empty Severity")
			assertNotEquals(t, "warning", receiver.PagerdutyConfigs[0].Severity, "Severity")
			assertEquals(t, proxy, receiver.PagerdutyConfigs[0].HttpConfig.ProxyURL, "Proxy")
		case receiverMakeItError:
			hasMakeItError = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertEquals(t, "error", receiver.PagerdutyConfigs[0].Severity, "Severity")
			assertNotEquals(t, "warning", receiver.PagerdutyConfigs[0].Severity, "Severity")
			assertEquals(t, proxy, receiver.PagerdutyConfigs[0].HttpConfig.ProxyURL, "Proxy")
		case receiverMakeItCritical:
			hasMakeItCritical = true
			assertEquals(t, true, receiver.PagerdutyConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.PagerdutyConfigs[0].RoutingKey, "RoutingKey")
			assertEquals(t, "critical", receiver.PagerdutyConfigs[0].Severity, "Severity")
			assertEquals(t, proxy, receiver.PagerdutyConfigs[0].HttpConfig.ProxyURL, "Proxy")
		}
	}

	assertTrue(t, hasMakeItCritical, fmt.Sprintf("No '%s' receiver", receiverMakeItCritical))
	assertTrue(t, hasMakeItError, fmt.Sprintf("No '%s' receiver", receiverMakeItError))
	assertTrue(t, hasMakeItWarning, fmt.Sprintf("No '%s' receiver", receiverMakeItWarning))
	assertTrue(t, hasPagerduty, fmt.Sprintf("No '%s' receiver", receiverPagerduty))
}

// utility function to verify Goalert Receivers
func verifyGoalertReceivers(t *testing.T, key string, proxy string, receivers []*alertmanager.Receiver) {
	// there are at least 3 receivers: namespace, elasticsearch, and fluentd
	assertGte(t, 2, len(receivers), "Number of Receivers")

	// verify structure of each
	hasGoalertLow := false
	hasGoalertHigh := false
	for _, receiver := range receivers {
		switch receiver.Name {
		case receiverGoAlertLow:
			hasGoalertLow = true
			assertEquals(t, true, receiver.WebhookConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.WebhookConfigs[0].URL, "URL")
			assertEquals(t, proxy, receiver.WebhookConfigs[0].HttpConfig.ProxyURL, "Proxy")
		case receiverGoAlertHigh:
			hasGoalertHigh = true
			assertEquals(t, true, receiver.WebhookConfigs[0].NotifierConfig.VSendResolved, "VSendResolved")
			assertEquals(t, key, receiver.WebhookConfigs[0].URL, "URL")
			assertEquals(t, proxy, receiver.WebhookConfigs[0].HttpConfig.ProxyURL, "Proxy")
		}
	}

	assertTrue(t, hasGoalertHigh, fmt.Sprintf("No '%s' receiver", receiverGoAlertHigh))
	assertTrue(t, hasGoalertLow, fmt.Sprintf("No '%s' receiver", receiverGoAlertLow))
}

// utility function to verify Goalert Heartbeat
func verifyHeartbeatRoute(t *testing.T, route *alertmanager.Route) {
	assertEquals(t, receiverGoAlertHeartbeat, route.Receiver, "Receiver Name")
	assertEquals(t, "5m", route.RepeatInterval, "Repeat Interval")
	assertEquals(t, "Watchdog", route.Match["alertname"], "Alert Name")
	assertEquals(t, true, route.Continue, "Continue")
}

// utility to test Goalert heartbeat receivers
func verifyHeartbeatReceiver(t *testing.T, url string, proxy string, receivers []*alertmanager.Receiver) {
	// there is 1 receiver
	assertGte(t, 1, len(receivers), "Number of Receivers")

	// verify structure of each
	hasWatchdog := false
	for _, receiver := range receivers {
		if receiver.Name == receiverGoAlertHeartbeat {
			hasWatchdog = true
			assertTrue(t, receiver.WebhookConfigs[0].VSendResolved, "VSendResolved")
			assertEquals(t, url, receiver.WebhookConfigs[0].URL, "URL")
			assertEquals(t, proxy, receiver.WebhookConfigs[0].HttpConfig.ProxyURL, "Proxy")
		}
	}

	assertTrue(t, hasWatchdog, fmt.Sprintf("No '%s' receiver", receiverWatchdog))
}

// utility function to verify watchdog route
func verifyWatchdogRoute(t *testing.T, route *alertmanager.Route) {
	assertEquals(t, receiverWatchdog, route.Receiver, "Receiver Name")
	assertEquals(t, "5m", route.RepeatInterval, "Repeat Interval")
	assertEquals(t, "Watchdog", route.Match["alertname"], "Alert Name")
	assertEquals(t, true, route.Continue, "Continue")
}

// utility to test watchdog receivers
func verifyWatchdogReceiver(t *testing.T, url string, proxy string, receivers []*alertmanager.Receiver) {
	// there is 1 receiver
	assertGte(t, 1, len(receivers), "Number of Receivers")

	// verify structure of each
	hasWatchdog := false
	for _, receiver := range receivers {
		if receiver.Name == receiverWatchdog {
			hasWatchdog = true
			assertTrue(t, receiver.WebhookConfigs[0].VSendResolved, "VSendResolved")
			assertEquals(t, url, receiver.WebhookConfigs[0].URL, "URL")
			assertEquals(t, proxy, receiver.WebhookConfigs[0].HttpConfig.ProxyURL, "Proxy")
		}
	}

	assertTrue(t, hasWatchdog, fmt.Sprintf("No '%s' receiver", receiverWatchdog))
}

// utility function to verify watchdog route
func verifyOCMAgentRoute(t *testing.T, route *alertmanager.Route) {
	assertEquals(t, receiverOCMAgent, route.Receiver, "Receiver Name")
	assertFalse(t, route.Continue, "Continue")
	assertEquals(t, "true", route.Match[managedNotificationLabel], "Alert Label")
}

// utility to test watchdog receivers
func verifyOCMAgentReceiver(t *testing.T, url string, receivers []*alertmanager.Receiver) {
	// there is 1 receiver
	assertGte(t, 1, len(receivers), "Number of Receivers")

	// verify structure of each
	hasOCMAgent := false
	for _, receiver := range receivers {
		if receiver.Name == receiverOCMAgent {
			hasOCMAgent = true
			assertTrue(t, receiver.WebhookConfigs[0].VSendResolved, "VSendResolved")
			assertEquals(t, url, receiver.WebhookConfigs[0].URL, "URL")
		}
	}

	assertTrue(t, hasOCMAgent, fmt.Sprintf("No '%s' receiver", receiverOCMAgent))
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
			Expected: true,
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
			Expected: true,
		},
		{
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
			Expected: true,
		},
		{
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
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "KubeNodeUnreachable",
			},
			TargetMatchRE: map[string]string{
				"alertname": "SDNPodNotReady|TargetDown",
			},
			Expected: true,
		},
		{
			Equal: []string{
				"instance",
			},
			SourceMatch: map[string]string{
				"alertname": "KubeNodeNotReady",
			},
			TargetMatchRE: map[string]string{
				"alertname": "KubeDaemonSetRolloutStuck|KubeDaemonSetMisScheduled|KubeDeploymentReplicasMismatch|KubeStatefulSetReplicasMismatch|KubePodNotReady",
			},
			Expected: true,
		},
		{
			Equal: []string{
				"namespace",
			},
			SourceMatch: map[string]string{
				"alertname": "KubeDeploymentReplicasMismatch",
			},
			TargetMatchRE: map[string]string{
				"alertname": "KubePodNotReady|KubePodCrashLooping",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "ElasticsearchOperatorCSVNotSuccessful",
			},
			TargetMatchRE: map[string]string{
				"alertname": "ElasticsearchClusterNotHealthy",
			},
			// NB this label obviously won't match and that's both ok and expected. When a label is missing (or empty) on both source and target, the rule will apply (see: docs ).
			// see: https://www.prometheus.io/docs/alerting/latest/configuration/#inhibit_rule

			// If there wasn't a label here, the tests exploded spectacularly, so I figured a label that would never match is the next best thing.
			Equal: []string{
				"dummylabel",
			},
			Expected: true,
		},
		{
			SourceMatch: map[string]string{
				"alertname": "KubeAPIErrorBudgetBurn",
			},
			TargetMatchRE: map[string]string{
				"alertname": "api-ErrorBudgetBurn",
			},
			Equal: []string{
				"severity",
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

func Test_cmInList(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createConfigMap(reconciler, cmNameManagedNamespaces, cmKeyManagedNamespaces, "test")

	cmList := corev1.ConfigMapList{}
	err := reconciler.Client.List(context.TODO(), &cmList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list ConfigMaps: %v", err)
	}

	assertTrue(t, cmInList(reqLogger, cmNameManagedNamespaces, &cmList), fmt.Sprintf("Expected ConfigMap to be present in list: %s", cmNameManagedNamespaces))
	assertTrue(t, !cmInList(reqLogger, "fake-configmap", &cmList), fmt.Sprintf("Did not expect ConfigMap to be present in list: %s", "fake-configmap"))
}

func Test_secretInList(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, "")
	createSecret(reconciler, secretNameDMS, secretKeyDMS, "")
	createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, "", "", "")

	secretList := corev1.SecretList{}
	err := reconciler.Client.List(context.TODO(), &secretList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list Secrets: %v", err)
	}

	assertTrue(t, secretInList(reqLogger, secretNamePD, &secretList), fmt.Sprintf("Expected Secret to be present in list: %s", secretNamePD))
	assertTrue(t, secretInList(reqLogger, secretNameDMS, &secretList), fmt.Sprintf("Expected Secret to be present in list: %s", secretNameDMS))
	assertTrue(t, secretInList(reqLogger, secretNameGoalert, &secretList), fmt.Sprintf("Expected Secret to be present in list: %s", secretNameGoalert))
	assertTrue(t, !secretInList(reqLogger, "fake-secret", &secretList), fmt.Sprintf("Did not expect Secret to be present in list: %s", "fake-secret"))
}

// Test_parseSecrets tests the parseSecrets function under normal circumstances
func Test_parseSecrets(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)

	pdKey := "asdfjkl123"
	dmsURL := "https://hjklasdf09876"
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createSecret(reconciler, secretNameDMS, secretKeyDMS, dmsURL)
	createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)

	secretList := &corev1.SecretList{}
	err := reconciler.Client.List(context.TODO(), secretList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list Secrets: %v", err)
	}

	request := createReconcileRequest(reconciler, secretNamePD)
	pagerdutyRoutingKey, watchdogURL, goalertURLlow, goalertURLhigh, goalertURLheartbeat := reconciler.parseSecrets(reqLogger, secretList, request.Namespace, true)

	assertEquals(t, pdKey, pagerdutyRoutingKey, "Expected PagerDuty routing keys to match")
	assertEquals(t, dmsURL, watchdogURL, "Expected DMS URLs to match")
	assertEquals(t, gaLowURL, goalertURLlow, "Expected GoAlert Low URLs to match")
	assertEquals(t, gaHighURL, goalertURLhigh, "Expected GoAlert High URLs to match")
	assertEquals(t, gaHeartURL, goalertURLheartbeat, "Expected GoAlert Heartbeat URLs to match")
}

// Test_parseSecrets tests the parseSecrets function when the DMS secret does not exist
func Test_parseSecrets_MissingDMS(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)

	pdKey := "asdfjkl123"

	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)

	secretList := &corev1.SecretList{}
	err := reconciler.Client.List(context.TODO(), secretList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list Secrets: %v", err)
	}

	request := createReconcileRequest(reconciler, secretNamePD)
	pagerdutyRoutingKey, watchdogURL, goalertURLlow, goalertURLhigh, goalertURLheartbeat := reconciler.parseSecrets(reqLogger, secretList, request.Namespace, true)

	assertEquals(t, pdKey, pagerdutyRoutingKey, "Expected PagerDuty routing keys to match")
	assertEquals(t, "", watchdogURL, "Expected DMS URLs to match")
	assertEquals(t, "", goalertURLlow, "Expected GoAlert Low URLs to match")
	assertEquals(t, "", goalertURLhigh, "Expected GoAlert High URLs to match")
	assertEquals(t, "", goalertURLheartbeat, "Expected GoAlert Heartbeat URLs to match")
}

// Tests the parseSecrets function when the PD secret does not exist
func Test_parseSecrets_MissingPagerDuty(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)

	dmsURL := "https://hjklasdf09876"

	createNamespace(reconciler, t)
	createSecret(reconciler, secretNameDMS, secretKeyDMS, dmsURL)

	secretList := &corev1.SecretList{}
	err := reconciler.Client.List(context.TODO(), secretList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list Secrets: %v", err)
	}

	request := createReconcileRequest(reconciler, secretNamePD)
	pagerdutyRoutingKey, watchdogURL, goalertURLlow, goalertURLhigh, goalertURLheartbeat := reconciler.parseSecrets(reqLogger, secretList, request.Namespace, true)

	assertEquals(t, "", pagerdutyRoutingKey, "Expected PagerDuty routing keys to match")
	assertEquals(t, dmsURL, watchdogURL, "Expected DMS URLs to match")
	assertEquals(t, "", goalertURLlow, "Expected GoAlert Low URLs to match")
	assertEquals(t, "", goalertURLhigh, "Expected GoAlert High URLs to match")
	assertEquals(t, "", goalertURLheartbeat, "Expected GoAlert Heartbeat URLs to match")
}

// Tests the parseSecrets function when the GoAlert secrets do not exist
func Test_parseSecrets_MissingGoAlert(t *testing.T) {
	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	reconciler := createReconciler(t, mockReadiness)

	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	createNamespace(reconciler, t)
	createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)

	secretList := &corev1.SecretList{}
	err := reconciler.Client.List(context.TODO(), secretList, &client.ListOptions{})
	if err != nil {
		t.Fatalf("Could not list Secrets: %v", err)
	}

	request := createReconcileRequest(reconciler, secretNameGoalert)
	pagerdutyRoutingKey, watchdogURL, goalertURLlow, goalertURLhigh, goalertURLheartbeat := reconciler.parseSecrets(reqLogger, secretList, request.Namespace, true)

	assertEquals(t, "", pagerdutyRoutingKey, "Expected PagerDuty routing keys to match")
	assertEquals(t, "", watchdogURL, "Expected DMS URLs to match")
	assertEquals(t, gaLowURL, goalertURLlow, "Expected GoAlert Low URLs to match")
	assertEquals(t, gaHighURL, goalertURLhigh, "Expected GoAlert High URLs to match")
	assertEquals(t, gaHeartURL, goalertURLheartbeat, "Expected GoAlert Heartbeat URLs to match")
}

// Test_parseConfigMaps tests the parseConfigMaps function under various circumstances
func Test_parseConfigMaps(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Generate aggregate list of example namespaces
	var validNamespaces []string
	validNamespaces = append(validNamespaces, exampleManagedNamespaces...)
	validNamespaces = append(validNamespaces, exampleOCPNamespaces...)

	// Convert to regex to match the result of parseConfigMaps()
	for i, ns := range validNamespaces {
		validNamespaces[i] = "^" + ns + "$"
	}

	type configMapTest struct {
		invalid bool
		missing bool
	}

	// Define tests
	tests := []struct {
		name               string
		expectedNamespaces []string
		managedNamespace   configMapTest
		ocpNamespaces      configMapTest
	}{
		{
			name:               "Valid configMaps",
			expectedNamespaces: validNamespaces,
			managedNamespace: configMapTest{
				invalid: false,
				missing: false,
			},
			ocpNamespaces: configMapTest{
				invalid: false,
				missing: false,
			},
		},
		{
			name:               "Invalid managed-namespaces configMap",
			expectedNamespaces: defaultNamespaces,
			managedNamespace: configMapTest{
				invalid: true,
				missing: false,
			},
			ocpNamespaces: configMapTest{
				invalid: false,
				missing: false,
			},
		},
		{
			name:               "Missing managed-namespaces configMap",
			expectedNamespaces: defaultNamespaces,
			managedNamespace: configMapTest{
				invalid: false,
				missing: true,
			},
			ocpNamespaces: configMapTest{
				invalid: false,
				missing: false,
			},
		},
		{
			name:               "Invalid ocp-namespaces configMap",
			expectedNamespaces: defaultNamespaces,
			managedNamespace: configMapTest{
				invalid: false,
				missing: false,
			},
			ocpNamespaces: configMapTest{
				invalid: true,
				missing: false,
			},
		},
		{
			name:               "Missing ocp-namespaces configMap",
			expectedNamespaces: defaultNamespaces,
			managedNamespace: configMapTest{
				invalid: false,
				missing: false,
			},
			ocpNamespaces: configMapTest{
				invalid: false,
				missing: true,
			},
		},
	}

	for _, tt := range tests {
		mockReadiness := readiness.NewMockInterface(ctrl)
		reconciler := createReconciler(t, mockReadiness)
		createNamespace(reconciler, t)

		// managed-namespaces configMap
		if !tt.managedNamespace.missing {
			var cmDataManagedNamespaces string
			if tt.managedNamespace.invalid {
				cmDataManagedNamespaces = "This is an invalid format for the managed-namespaces configmap!"
			} else {
				cmDataManagedNamespaces = "Resources:\n  Namespace:"
				for _, ns := range exampleManagedNamespaces {
					cmDataManagedNamespaces = cmDataManagedNamespaces + fmt.Sprintf("\n  - name: '%v'", ns)
				}
			}
			createConfigMap(reconciler, cmNameManagedNamespaces, cmKeyManagedNamespaces, cmDataManagedNamespaces)
		}

		// ocp-namespaces configMap
		if !tt.ocpNamespaces.missing {
			var cmDataOcpNamespaces string
			if !tt.ocpNamespaces.invalid {
				cmDataOcpNamespaces = "Resources:\n  Namespace:"
				for _, ns := range exampleOCPNamespaces {
					cmDataOcpNamespaces = cmDataOcpNamespaces + fmt.Sprintf("\n  - name: '%v'", ns)
				}
			} else {
				cmDataOcpNamespaces = "This is an invalid format for the managed-namespaces configmap!"
			}
			createConfigMap(reconciler, cmNameOCPNamespaces, cmKeyOCPNamespaces, cmDataOcpNamespaces)
		}

		// Run and verify results
		cmList := &corev1.ConfigMapList{}
		err := reconciler.Client.List(context.TODO(), cmList, &client.ListOptions{})
		if err != nil {
			t.Fatalf("Could not list ConfigMaps: %v", err)
		}

		request := createReconcileRequest(reconciler, cmNameManagedNamespaces)
		namespaceList := reconciler.parseConfigMaps(reqLogger, cmList, request.Namespace)

		assertEquals(t, tt.expectedNamespaces, namespaceList, "Expected namespace lists to match")
	}
}

// Test_parseConfigMaps tests the parseConfigMaps function under various circumstances
func Test_readOCMAgentServiceURLFromConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	type configMapTest struct {
		invalid bool
		missing bool
	}

	testValidURL := "http://a-valid.url.svc.cluster.local:9999/test"
	// Define tests
	tests := []struct {
		name               string
		expectedServiceURL string
		oaConfigMap        configMapTest
	}{
		{
			name:               "Valid configMap",
			expectedServiceURL: testValidURL,
			oaConfigMap: configMapTest{
				invalid: false,
				missing: false,
			},
		},
		{
			name:               "Invalid configMap",
			expectedServiceURL: "",
			oaConfigMap: configMapTest{
				invalid: true,
				missing: false,
			},
		},
		{
			name:               "Missing configMap",
			expectedServiceURL: "",
			oaConfigMap: configMapTest{
				invalid: false,
				missing: true,
			},
		},
	}

	for _, tt := range tests {
		mockReadiness := readiness.NewMockInterface(ctrl)
		reconciler := createReconciler(t, mockReadiness)
		createNamespace(reconciler, t)

		// managed-namespaces configMap
		if !tt.oaConfigMap.missing {
			var cmDataOCMAgentServiceURL string
			if tt.oaConfigMap.invalid {
				cmDataOCMAgentServiceURL = "This is an invalid URL"
			} else {
				cmDataOCMAgentServiceURL = testValidURL
			}
			createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, cmDataOCMAgentServiceURL)
		}

		// Run and verify results
		cmList := &corev1.ConfigMapList{}
		err := reconciler.Client.List(context.TODO(), cmList, &client.ListOptions{})
		if err != nil {
			t.Fatalf("Could not list ConfigMaps: %v", err)
		}

		request := createReconcileRequest(reconciler, cmNameOcmAgent)
		oaService := reconciler.readOCMAgentServiceURLFromConfig(reqLogger, cmList, request.Namespace)

		assertEquals(t, tt.expectedServiceURL, oaService, "Expected OCM Agent service URLs to match")
	}
}

func Test_createPagerdutyRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createPagerdutyRoute(defaultNamespaces)

	verifyPagerdutyRoute(t, route, defaultNamespaces)
}

func Test_createGoalertRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createPagerdutyRoute(defaultNamespaces)

	verifyGoalertRoute(t, route, defaultNamespaces)
}

func Test_createPagerdutyReceivers_WithoutKey(t *testing.T) {
	assertEquals(t, 0, len(createPagerdutyReceivers("", "", "")), "Number of Receivers")
}

func Test_createPagerdutyReceivers_WithKey(t *testing.T) {
	key := "abcdefg1234567890"

	receivers := createPagerdutyReceivers(key, exampleClusterId, exampleProxy)

	verifyPagerdutyReceivers(t, key, exampleProxy, receivers)
}

func Test_createWatchdogRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createWatchdogRoute()

	verifyWatchdogRoute(t, route)
}

func Test_createHeartbeatReceivers_WithoutURL(t *testing.T) {
	assertEquals(t, 0, len(createHeartbeatReceivers("", "")), "Number of Receivers")
}

func Test_createHeartbeatReceivers_WithKey(t *testing.T) {
	url := "http://whatever/something"

	receivers := createHeartbeatReceivers(url, exampleProxy)

	verifyHeartbeatReceiver(t, url, exampleProxy, receivers)
}

func Test_createHeartbeatRoute(t *testing.T) {
	// test the structure of the Route is sane
	route := createHeartbeatRoute()

	verifyHeartbeatRoute(t, route)
}

func Test_createWatchdogReceivers_WithoutURL(t *testing.T) {
	assertEquals(t, 0, len(createWatchdogReceivers("", "")), "Number of Receivers")
}

func Test_createWatchdogReceivers_WithKey(t *testing.T) {
	url := "http://whatever/something"

	receivers := createWatchdogReceivers(url, exampleProxy)

	verifyWatchdogReceiver(t, url, exampleProxy, receivers)
}
func Test_createAlertManagerConfig_WithoutKey_WithoutURL(t *testing.T) {
	pdKey := ""
	wdURL := ""
	oaURL := ""
	gaHighURL := ""
	gaLowURL := ""
	gaHeartURL := ""

	config := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, exampleManagedNamespaces)

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
	oaURL := ""
	gaHighURL := ""
	gaLowURL := ""
	gaHeartURL := ""

	config := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, exampleManagedNamespaces)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 1, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 5, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)

	verifyPagerdutyRoute(t, config.Route.Routes[0], exampleManagedNamespaces)
	verifyPagerdutyReceivers(t, pdKey, exampleProxy, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

func Test_createAlertManagerConfig_WithKey_WithWDURL_WithOAURL(t *testing.T) {
	pdKey := "poiuqwer78902345"
	wdURL := "http://theinterwebs"
	oaURL := "https://dummy-oa-url"
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	config := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, exampleManagedNamespaces)

	// verify static things
	assertEquals(t, "5m", config.Global.ResolveTimeout, "Global.ResolveTimeout")
	assertEquals(t, pagerdutyURL, config.Global.PagerdutyURL, "Global.PagerdutyURL")
	assertEquals(t, defaultReceiver, config.Route.Receiver, "Route.Receiver")
	assertEquals(t, "30s", config.Route.GroupWait, "Route.GroupWait")
	assertEquals(t, "5m", config.Route.GroupInterval, "Route.GroupInterval")
	assertEquals(t, "12h", config.Route.RepeatInterval, "Route.RepeatInterval")
	assertEquals(t, 3, len(config.Route.Routes), "Route.Routes")
	assertEquals(t, 7, len(config.Receivers), "Receivers")

	verifyNullReceiver(t, config.Receivers)

	verifyPagerdutyRoute(t, config.Route.Routes[2], exampleManagedNamespaces)
	verifyPagerdutyReceivers(t, pdKey, exampleProxy, config.Receivers)

	verifyGoalertRoute(t, config.Route.Routes[3], exampleManagedNamespaces)
	verifyGoalertReceivers(t, pdKey, exampleProxy, config.Receivers)

	verifyWatchdogRoute(t, config.Route.Routes[0])
	verifyWatchdogReceiver(t, wdURL, exampleProxy, config.Receivers)

	verifyOCMAgentRoute(t, config.Route.Routes[1])
	verifyOCMAgentReceiver(t, oaURL, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

func Test_createAlertManagerConfig_WithoutKey_WithoutOA_WithWDURL(t *testing.T) {
	pdKey := ""
	wdURL := "http://theinterwebs"
	oaURL := ""
	gaHighURL := ""
	gaLowURL := ""
	gaHeartURL := ""

	config := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, exampleManagedNamespaces)

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
	verifyWatchdogReceiver(t, wdURL, exampleProxy, config.Receivers)

	verifyInhibitRules(t, config.InhibitRules)
}

// createConfigMap creates a fake ConfigMap to use in testing.
func createConfigMap(reconciler *SecretReconciler, configMapName string, configMapKey string, configMapData string) {
	newconfigmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: config.OperatorNamespace,
		},
		Data: map[string]string{
			configMapKey: configMapData,
		},
	}
	if err := reconciler.Client.Create(context.TODO(), newconfigmap); err != nil {
		panic(err)
	}
}

// createSecret creates a fake Secret to use in testing.
func createSecret(reconciler *SecretReconciler, secretname string, secretkey string, secretdata string) {
	newsecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretname,
			Namespace: config.OperatorNamespace,
		},
		Data: map[string][]byte{
			secretkey: []byte(secretdata),
		},
	}
	if err := reconciler.Client.Create(context.TODO(), newsecret); err != nil {
		panic(err)
	}
}

// createSecret creates a fake Secret to use in testing GoAlert. GoAlert has 3 values in a single secret
func createGoAlertSecret(reconciler *SecretReconciler, secretname string, secretkeyLow string, secretkeyHigh string, secretkeyHeartbeat string, secretdataLow string, secretdataHigh string, secretdataHeartbeat string) {
	newsecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretname,
			Namespace: config.OperatorNamespace,
		},
		Data: map[string][]byte{
			secretkeyLow: []byte(secretdataLow),
			secretkeyHigh: []byte(secretdataHigh),
			secretkeyHeartbeat: []byte(secretdataHeartbeat),
		},
	}
	if err := reconciler.Client.Create(context.TODO(), newsecret); err != nil {
		panic(err)
	}
}

// createReconciler creates a fake SecretReconciler for testing.
// If ready is nil, a real readiness Impl is constructed.
func createReconciler(t *testing.T, ready readiness.Interface) *SecretReconciler {
	// scheme := scheme.Scheme
	fakeScheme := k8sruntime.NewScheme()
	utilruntime.Must(configv1.AddToScheme(fakeScheme))
	utilruntime.Must(corev1.AddToScheme(fakeScheme))
	utilruntime.Must(monitoringv1.AddToScheme(fakeScheme))

	// if err := configv1.AddToScheme(scheme); err != nil {
	// 	t.Fatalf("Unable to add route scheme: (%v)", err)
	// }
	if ready == nil {
		ready = &readiness.Impl{}
	}

	return &SecretReconciler{
		Client:    fake.NewClientBuilder().WithScheme(fakeScheme).Build(),
		Scheme:    fakeScheme,
		Readiness: ready,
	}
}

// createNamespace creates a fake `openshift-monitoring` namespace for testing.
func createNamespace(reconciler *SecretReconciler, t *testing.T) {
	err := reconciler.Client.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: config.OperatorNamespace}})
	if err != nil {
		// exit the test if we can't create the namespace. Every test depends on this.
		t.Errorf("Couldn't create the required namespace for the test. Encountered error: %s", err)
		panic("Exiting due to fatal error")
	}
}

// Create the reconcile request for the specified secret.
func createReconcileRequest(reconciler *SecretReconciler, secretname string) *reconcile.Request {
	return &reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      secretname,
			Namespace: config.OperatorNamespace,
		},
	}
}

// Test_createPagerdutySecret_Create tests writing to the Alertmanager config.
func Test_createPagerdutySecret_Create(t *testing.T) {
	pdKey := "asdaidsgadfi9853"
	wdURL := "http://theinterwebs/asdf"
	oaURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	mockReadiness.EXPECT().IsReady().Times(1).Return(true, nil)
	mockReadiness.EXPECT().Result().Times(1).Return(reconcile.Result{})
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
	createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
	createClusterVersion(reconciler)
	createClusterProxy(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, "pd-secret")
	ret, err := reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// read config and a copy for comparison
	configActual := readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

// Test updating the config and making sure it is updated as expected
func Test_createPagerdutySecret_Update(t *testing.T) {
	pdKey := "asdaidsgadfi9853"
	wdURL := "http://theinterwebs/asdf"
	oaURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	var ret reconcile.Result
	var err error

	configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	mockReadiness.EXPECT().IsReady().Times(2).Return(true, nil)
	mockReadiness.EXPECT().Result().Times(2).Return(reconcile.Result{})
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
	createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
	createClusterVersion(reconciler)
	createClusterProxy(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, secretNamePD)
	ret, err = reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// verify what we have configured is NOT what we expect at the end (we have updates to do still)
	configActual := readAlertManagerConfig(reconciler, req)
	assertNotEquals(t, configExpected, configActual, "Config Deep Comparison")

	// update environment
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
	req = createReconcileRequest(reconciler, secretNameDMS)
	ret, err = reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// read config and compare
	configActual = readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

// Test_createPagerdutySecret_Create tests writing to the Alertmanager config.
func Test_createGoalertSecret_Create(t *testing.T) {
	pdKey := "fakekey"
	wdURL := "http://theinterwebs/asdf"
	oaURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	mockReadiness.EXPECT().IsReady().Times(1).Return(true, nil)
	mockReadiness.EXPECT().Result().Times(1).Return(reconcile.Result{})
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)
	createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
	createClusterVersion(reconciler)
	createClusterProxy(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, "goalert-secret")
	ret, err := reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// read config and a copy for comparison
	configActual := readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

// Test updating the config and making sure it is updated as expected
func Test_createGoalertSecret_Update(t *testing.T) {
	pdKey := "fakekey"
	wdURL := "http://theinterwebs/asdf"
	oaURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
	gaHighURL := "https://dummy-gahigh-url"
	gaLowURL := "https://dummy-galow-url"
	gaHeartURL := "https://dummy-gaheartbeat-url"

	var ret reconcile.Result
	var err error

	configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

	verifyInhibitRules(t, configExpected.InhibitRules)

	// prepare environment
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockReadiness := readiness.NewMockInterface(ctrl)
	mockReadiness.EXPECT().IsReady().Times(2).Return(true, nil)
	mockReadiness.EXPECT().Result().Times(2).Return(reconcile.Result{})
	reconciler := createReconciler(t, mockReadiness)
	createNamespace(reconciler, t)
	createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)
	createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
	createClusterVersion(reconciler)
	createClusterProxy(reconciler)

	// reconcile (one event should config everything)
	req := createReconcileRequest(reconciler, secretNamePD)
	ret, err = reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// verify what we have configured is NOT what we expect at the end (we have updates to do still)
	configActual := readAlertManagerConfig(reconciler, req)
	assertNotEquals(t, configExpected, configActual, "Config Deep Comparison")

	// update environment
	createSecret(reconciler, secretNameDMS, secretKeyDMS, wdURL)
	req = createReconcileRequest(reconciler, secretNameDMS)
	ret, err = reconciler.Reconcile(context.TODO(), *req)
	assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
	assertEquals(t, nil, err, "Unexpected err")

	// read config and compare
	configActual = readAlertManagerConfig(reconciler, req)

	assertEquals(t, configExpected, configActual, "Config Deep Comparison")
}

func createClusterProxy(reconciler *SecretReconciler) {
	clusterProxy := &configv1.Proxy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.ProxySpec{
			HTTPSProxy: exampleProxy,
		},
		Status: configv1.ProxyStatus{
			HTTPSProxy: exampleProxy,
		},
	}
	if err := reconciler.Client.Create(context.TODO(), clusterProxy); err != nil {
		panic(err)
	}
}

func createClusterVersion(reconciler *SecretReconciler) {
	clusterVersion := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Spec: configv1.ClusterVersionSpec{
			ClusterID: exampleClusterId,
		},
	}
	if err := reconciler.Client.Create(context.TODO(), clusterVersion); err != nil {
		panic(err)
	}
}

func Test_SecretReconciler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name        string
		dmsExists   bool
		pdExists    bool
		amExists    bool
		oaExists    bool
		gaExists	bool
		otherExists bool
	}{
		{
			name:        "Test reconcile with NO secrets.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    false,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with dms-secret only.",
			dmsExists:   true,
			pdExists:    false,
			amExists:    false,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd-secret only.",
			dmsExists:   false,
			pdExists:    true,
			amExists:    false,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with alertmanager-main only.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    true,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with GoALert secret only.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    false,
			oaExists:    false,
			gaExists:    true,
			otherExists: false,
		},
		{
			name:        "Test reconcile with 'other' secret only.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    false,
			oaExists:    false,
			gaExists:    false,
			otherExists: true,
		},
		{
			name:        "Test reconcile with pd & dms secrets.",
			dmsExists:   true,
			pdExists:    true,
			amExists:    false,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd & am secrets.",
			dmsExists:   false,
			pdExists:    true,
			amExists:    true,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with ga & am secrets.",
			dmsExists:   false,
			pdExists:    false,
			amExists:    true,
			oaExists:    false,
			gaExists:    true,
			otherExists: false,
		},
		{
			name:        "Test reconcile with am & dms secrets.",
			dmsExists:   true,
			pdExists:    false,
			amExists:    true,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with pd, dms, and am secrets.",
			dmsExists:   true,
			pdExists:    true,
			amExists:    true,
			oaExists:    false,
			gaExists:    false,
			otherExists: false,
		},
		{
			name:        "Test reconcile with ga, pd, dms, and am secrets.",
			dmsExists:   true,
			pdExists:    true,
			amExists:    true,
			oaExists:    false,
			gaExists:    true,
			otherExists: false,
		},
	}
	for _, tt := range tests {
		mockReadiness := readiness.NewMockInterface(ctrl)
		mockReadiness.EXPECT().IsReady().Times(1).Return(true, nil)
		mockReadiness.EXPECT().Result().Times(1).Return(reconcile.Result{})
		reconciler := createReconciler(t, mockReadiness)
		createNamespace(reconciler, t)
		createClusterVersion(reconciler)
		createClusterProxy(reconciler)

		pdKey := ""
		wdURL := ""
		oaURL := ""
		gaLowURL := ""
		gaHighURL := ""
		gaHeartURL := ""

		// Create the secrets for this specific test.
		if tt.amExists {
			writeAlertManagerConfig(reconciler, reqLogger, createAlertManagerConfig("", "", "", "", "", "", "", "", defaultNamespaces))
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
		if tt.gaExists {
			gaHighURL := "https://dummy-gahigh-url"
			gaLowURL := "https://dummy-galow-url"
			gaHeartURL := "https://dummy-gaheartbeat-url"
			createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)
		}
		if tt.oaExists {
			oaURL = fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
			createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
		}
		configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, wdURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

		verifyInhibitRules(t, configExpected.InhibitRules)

		req := createReconcileRequest(reconciler, secretNameAlertmanager)
		ret, err := reconciler.Reconcile(context.TODO(), *req)
		assertEquals(t, reconcile.Result{}, ret, "Unexpected result")
		assertEquals(t, nil, err, "Unexpected err")

		// load the config and check it
		configActual := readAlertManagerConfig(reconciler, req)

		// NOTE compare of the objects will fail when no secrets are created for some reason, so using .String()
		assertEquals(t, configExpected.String(), configActual.String(), tt.name)
	}
}

// Test_SecretReconciler_Readiness tests the Reconcile loop for different results of the
// cluster readiness check.
func Test_SecretReconciler_Readiness(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	tests := []struct {
		name      string
		ready     bool
		readyErr  bool
		expectDMS bool
		expectPD  bool
		expectGA  bool
		expectOA  bool
	}{
		{
			name:      "Cluster not ready: don't configure GA, PD or OA.",
			ready:     false,
			readyErr:  false,
			expectDMS: true,
			expectPD:  false,
			expectGA:  false,
			expectOA:  false,
		},
		{
			// This is covered by other test cases, but for completeness...
			name:      "Cluster ready: configure everything.",
			ready:     true,
			readyErr:  false,
			expectDMS: true,
			expectPD:  true,
			expectGA:  true,
			expectOA:  true,
		},
		{
			name:      "Readiness check errors: don't configure anything.",
			ready:     false,
			readyErr:  true,
			expectDMS: false,
			expectPD:  false,
			expectGA:  false,
			expectOA:  false,
		},
	}
	for _, tt := range tests {
		mockReadiness := readiness.NewMockInterface(ctrl)
		var expectErr error = nil
		if tt.readyErr {
			expectErr = fmt.Errorf("An error occurred")
		}
		mockReadiness.EXPECT().IsReady().Times(1).Return(tt.ready, expectErr)
		// Use a weird Result() to validate that all the code paths are using it.
		expectResult := reconcile.Result{RequeueAfter: 12345}
		mockReadiness.EXPECT().Result().Times(1).Return(expectResult)
		reconciler := createReconciler(t, mockReadiness)
		createNamespace(reconciler, t)
		createClusterVersion(reconciler)
		createClusterProxy(reconciler)

		writeAlertManagerConfig(reconciler, reqLogger, createAlertManagerConfig("", "", "", "", "", "", "", "", defaultNamespaces))

		pdKey := "asdfjkl123"
		dmsURL := "https://hjklasdf09876"
		oaURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", ocmAgentService, ocmAgentNamespace, 9999, ocmAgentWebhookPath)
		gaHighURL := "https://dummy-gahigh-url"
		gaLowURL := "https://dummy-galow-url"
		gaHeartURL := "https://dummy-gaheartbeat-url"

		// Create the secrets for this specific test.
		// We're testing that Reconcile parlays the PD/DMS secrets into the AM config as
		// appropriate. So we always start with those two secrets
		createSecret(reconciler, secretNameDMS, secretKeyDMS, dmsURL)
		createSecret(reconciler, secretNamePD, secretKeyPD, pdKey)
		createGoAlertSecret(reconciler, secretNameGoalert, secretKeyGoalertLow, secretKeyGoalertHigh, secretKeyGoalertHeartbeat, gaLowURL, gaHighURL, gaHeartURL)

		// However, we expect the AM config to be updated only according to the test spec
		if !tt.expectDMS {
			dmsURL = ""
		}
		if !tt.expectPD {
			pdKey = ""
		}
		if !tt.expectGA {
			gaHighURL = ""
			gaLowURL = ""
			gaHeartURL = ""
		}
		if tt.expectOA {
			createConfigMap(reconciler, cmNameOcmAgent, cmKeyOCMAgent, oaURL)
		} else {
			oaURL = ""
		}
		configExpected := createAlertManagerConfig(pdKey, gaLowURL, gaHighURL, gaHeartURL, dmsURL, oaURL, exampleClusterId, exampleProxy, defaultNamespaces)

		verifyInhibitRules(t, configExpected.InhibitRules)

		req := createReconcileRequest(reconciler, secretNameAlertmanager)
		ret, err := reconciler.Reconcile(context.TODO(), *req)
		assertEquals(t, expectResult, ret, "Unexpected result")
		assertEquals(t, expectErr, err, "Unexpected err")

		// load the config and check it
		configActual := readAlertManagerConfig(reconciler, req)

		// NOTE compare of the objects will fail when no secrets are created for some reason, so using .String()
		assertEquals(t, configExpected.String(), configActual.String(), tt.name)
	}
}
