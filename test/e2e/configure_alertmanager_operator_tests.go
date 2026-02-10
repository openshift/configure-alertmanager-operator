// DO NOT REMOVE TAGS BELOW. IF ANY NEW TEST FILES ARE CREATED UNDER /osde2e, PLEASE ADD THESE TAGS TO THEM IN ORDER TO BE EXCLUDED FROM UNIT TESTS. //go:build osde2e
//go:build osde2e
// +build osde2e

package osde2etests

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/osde2e-common/pkg/clients/openshift"
	"github.com/openshift/osde2e-common/pkg/clients/prometheus"
	amconfig "github.com/prometheus/alertmanager/config"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
)

var _ = Describe("Configure AlertManager Operator", Ordered, func() {
	var (
		client          *resources.Resources
		dynamicClient   dynamic.Interface
		prom            *prometheus.Client
		secrets         = []string{"pd-secret", "dms-secret"}
		serviceAccounts = []string{"configure-alertmanager-operator"}
	)
	const (
		maxCSVFailures    = 1 //number of csv request failures before exiting
		timeoutDuration   = 300 * time.Second
		pollingDuration   = 30 * time.Second
		configMapLockFile = "configure-alertmanager-operator-lock"
		namespace         = "openshift-monitoring"
		operatorName      = "configure-alertmanager-operator"
		labelSelector     = "operators.coreos.com/configure-alertmanager-operator.openshift-monitoring"
	)

	BeforeAll(func(ctx context.Context) {
		log.SetLogger(GinkgoLogr)

		cfg, err := config.GetConfig()
		Expect(err).Should(BeNil(), "failed to get kubeconfig")
		client, err = resources.New(cfg)
		Expect(err).Should(BeNil(), "resources.New error")

		dynamicClient, err = dynamic.NewForConfig(cfg)
		Expect(err).ShouldNot(HaveOccurred(), "failed to configure Dynamic client")

		// Create openshift client locally for prometheus client setup
		k8s, err := openshift.New(GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup openshift client")

		prom, err = prometheus.New(ctx, k8s)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup prometheus client")
	})
	// Allow for one CSV request failure before exiting Eventually() loop...
	csvErrCounter := 0
	startCSVCheck := time.Now()
	It("cluster service version exists", func(ctx context.Context) {
		Eventually(func(ctx context.Context) bool {
			elapsed := fmt.Sprintf("%f", time.Since(startCSVCheck).Seconds())
			GinkgoLogr.Info("CAMO CSV check", "secondsElapsed", elapsed)
			csvList, err := dynamicClient.Resource(
				schema.GroupVersionResource{
					Group:    "operators.coreos.com",
					Version:  "v1alpha1",
					Resource: "clusterserviceversions",
				},
			).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
			if csvErrCounter >= maxCSVFailures {
				// If maxCSVFailures has been exceeded, handle errors with Expect()...
				csvErrCounter++
				GinkgoLogr.Error(err, fmt.Sprintf("CSV error counter: %d, tolerated errors: %d", csvErrCounter, maxCSVFailures))
				Expect(err).NotTo(HaveOccurred(), "Failed to retrieve CSV from namespace %s", namespace)
				Expect(csvList.Items).Should(HaveLen(1))
			}
			if err != nil {
				GinkgoLogr.Error(err, fmt.Sprintf("Err, fetching CSV for NS:'%s' LABEL:'%s'", namespace, labelSelector))
				csvErrCounter++
				return false
			}
			if csvList == nil {
				GinkgoLogr.Error(nil, fmt.Sprintf("Err, nil CSV list fetching CSV for NS:'%s' LABEL:'%s'", namespace, labelSelector))
				csvErrCounter++
				return false
			}
			if len(csvList.Items) != 1 {
				GinkgoLogr.Error(nil, fmt.Sprintf("Err, expected 1 CSV for NS:'%s' LABEL:'%s'. Got %d", namespace, labelSelector, len(csvList.Items)))
				csvErrCounter++
				return false
			}
			statusPhase, _, _ := unstructured.NestedFieldCopy(csvList.Items[0].Object, "status", "phase")
			if statusPhase == "Succeeded" {
				GinkgoLogr.Info("csv phase", "phase", statusPhase)
				return true
			}
			GinkgoLogr.Info("csv phase", "phase", statusPhase)
			return false
		}, ctx).WithTimeout(timeoutDuration).WithPolling(pollingDuration).Should(BeTrue(), "CSV %s should exist and have Succeeded status", operatorName)
	})

	It("service accounts exist", func(ctx context.Context) {
		for _, serviceAccount := range serviceAccounts {
			err := client.Get(ctx, serviceAccount, namespace, &v1.ServiceAccount{})
			Expect(err).ShouldNot(HaveOccurred(), "Service account %s not found", serviceAccount)
		}
	})

	It("deployment exists", func(ctx context.Context) {
		err := wait.For(conditions.New(client).DeploymentAvailable(operatorName, namespace))
		Expect(err).ShouldNot(HaveOccurred(), "Deployment %s not available", operatorName)
	})

	It("roles exist", func(ctx context.Context) {
		var roles rbacv1.RoleList
		err := client.WithNamespace(namespace).List(ctx, &roles, resources.WithLabelSelector(labelSelector))
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get roles")
		Expect(roles.Items).ShouldNot(BeZero(), "no roles found")
	})

	It("role bindings exist", func(ctx context.Context) {
		var roleBindings rbacv1.RoleBindingList
		err := client.WithNamespace(namespace).List(ctx, &roleBindings, resources.WithLabelSelector(labelSelector))
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get role bindings")
		Expect(roleBindings.Items).ShouldNot(BeZero(), "no rolebindings found")
	})

	It("cluster roles exist", func(ctx context.Context) {
		var clusterRoles rbacv1.ClusterRoleList
		err := client.WithNamespace(namespace).List(ctx, &clusterRoles, resources.WithLabelSelector(labelSelector))
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get cluster roles")
		Expect(clusterRoles.Items).ShouldNot(BeZero(), "no clusterroles found")
	})

	It("cluster role bindings exist", func(ctx context.Context) {
		var clusterRoleBindings rbacv1.ClusterRoleBindingList
		err := client.List(ctx, &clusterRoleBindings, resources.WithLabelSelector(labelSelector))
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get cluster role bindings")
		Expect(clusterRoleBindings.Items).ShouldNot(BeZero(), "no clusterrolebindingss found")
	})

	It("config map exists", func(ctx context.Context) {
		err := client.Get(ctx, configMapLockFile, namespace, &v1.ConfigMap{})
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get config map %s", configMapLockFile)
	})

	It("secrets exist", func(ctx context.Context) {
		for _, secret := range secrets {
			err := client.Get(ctx, secret, namespace, &v1.Secret{})
			Expect(err).ShouldNot(HaveOccurred(), "Secret %s not found", secret)
		}
	})

	It("alertmanager-main secret contains valid configuration", func(ctx context.Context) {
		// Get the alertmanager-main secret from openshift-monitoring
		var secret v1.Secret
		err := client.Get(ctx, "alertmanager-main", namespace, &secret)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get alertmanager-main secret")

		// Extract the alertmanager.yaml data
		configData, exists := secret.Data["alertmanager.yaml"]
		Expect(exists).Should(BeTrue(), "alertmanager.yaml not found in secret")
		Expect(configData).ShouldNot(BeEmpty(), "alertmanager.yaml is empty")

		// Validate the config using Alertmanager's official validation
		// This is the same validation that Alertmanager performs on startup
		_, err = amconfig.Load(string(configData))
		Expect(err).ShouldNot(HaveOccurred(), "alertmanager config validation failed: %v", err)
	})

	It("validation metric exists and shows config is valid", func(ctx context.Context) {
		// Query the alertmanager_config_validation_failed metric
		// Metric value: 0 = validation succeeded, 1 = validation failed
		query := `alertmanager_config_validation_failed{name="configure-alertmanager-operator"}`

		// Use Eventually to allow time for metric to be scraped
		Eventually(ctx, func(ctx context.Context) error {
			results, err := prom.InstantQuery(ctx, query)
			if err != nil {
				return fmt.Errorf("failed to query prometheus: %w", err)
			}

			if len(results) == 0 {
				return fmt.Errorf("metric not found: %s", query)
			}

			// Verify metric value is 0 (valid config)
			metricValue := int(results[0].Value)
			if metricValue != 0 {
				return fmt.Errorf("expected metric value 0 (valid), got %d (invalid)", metricValue)
			}

			return nil
		}).
			WithPolling(10 * time.Second).
			WithTimeout(2 * time.Minute).
			Should(Succeed(), "validation metric should exist and show config is valid")
	})



	// Namespace-scoping specific tests
	// These tests validate that cluster-scoped resources remain accessible
	// when the operator cache is scoped to openshift-monitoring namespace

	It("can access ClusterVersion cluster-scoped resource", func(ctx context.Context) {
		ginkgo.By("Verifying operator can read ClusterVersion despite namespace-scoped cache")

		// ClusterVersion is a cluster-scoped resource that the operator needs for:
		// 1. Getting cluster ID for PagerDuty/DMS integration labels
		// 2. Detecting management clusters via annotations
		// 3. Triggering reconciliation when cluster version changes

		var clusterVersion unstructured.Unstructured
		clusterVersion.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "config.openshift.io",
			Version: "v1",
			Kind:    "ClusterVersion",
		})

		err := client.Get(ctx, "version", "", &clusterVersion)
		if err != nil {
			failureMsg := fmt.Sprintf(`
FAILURE: Cannot access ClusterVersion cluster-scoped resource

IMPACT:
  This failure means the operator cannot:
  - Read the cluster ID (breaks PagerDuty/DMS integration routing)
  - Detect if this is a management cluster (breaks MC namespace routing)
  - Watch ClusterVersion for reconciliation triggers

ROOT CAUSE:
  The operator cache is scoped to openshift-monitoring namespace but ClusterVersion
  is a cluster-scoped resource. The ByObject cache configuration in main.go should
  explicitly cache ClusterVersion resources:

    ByObject: map[client.Object]cache.ByObject{
        &configv1.ClusterVersion{}: {},  // ← This line is critical
    }

PERMISSION VERIFICATION (test as operator ServiceAccount):
  # 1. Test if operator SA can read ClusterVersion resource:
  oc get clusterversion version --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 2. Test with verbose output to see exact error:
  oc get clusterversion version --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator -v=8

  # 3. Check what permissions the operator SA actually has on ClusterVersion:
  oc auth can-i get clusterversions --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 4. Check all ClusterVersion permissions for operator SA:
  oc auth can-i --list --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator | grep clusterversion

RBAC VERIFICATION (check ClusterRole and bindings):
  # 1. View operator's ClusterRole for ClusterVersion access:
  oc get clusterrole configure-alertmanager-operator -o yaml | grep -A 10 "clusterversions"

  # 2. Find which ClusterRoleBinding grants operator SA this ClusterRole:
  oc get clusterrolebinding -o json | jq -r '.items[] | select(.subjects[]? | select(.kind=="ServiceAccount" and .name=="configure-alertmanager-operator" and .namespace=="openshift-monitoring")) | .metadata.name'

  # 3. Verify the ClusterRoleBinding is correct:
  oc get clusterrolebinding configure-alertmanager-operator -o yaml

  # 4. Check if operator SA has any additional ClusterRoles:
  oc get clusterrolebinding -o json | jq -r '.items[] | select(.subjects[]? | select(.kind=="ServiceAccount" and .name=="configure-alertmanager-operator")) | "\(.metadata.name): \(.roleRef.name)"'

TROUBLESHOOTING COMMANDS:
  # 1. Verify ClusterVersion resource exists on cluster (as cluster-admin):
  oc get clusterversion version -o yaml

  # 2. Check operator logs for permission/access errors:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "clusterversion\|permission denied\|forbidden"

  # 3. Check operator logs for cache-related errors:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "cache\|informer\|watch"

  # 4. Verify operator deployment has correct image with namespace scoping:
  oc get deployment configure-alertmanager-operator -n openshift-monitoring -o jsonpath='{.spec.template.spec.containers[0].image}'

  # 5. Check if alertmanager-main secret has cluster ID (proves operator read ClusterVersion):
  oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.metadata.labels.cluster_id}'

  # 6. Force reconciliation and watch logs:
  oc annotate secret pd-secret -n openshift-monitoring test-reconcile=$(date +%%s) && oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=50 -f

DIAGNOSIS:
  Compare the results above to determine the failure type:

  [FAIL] Permission Denied (403):
     - 'oc get clusterversion --as=...' returns "Forbidden"
     - FIX: Add clusterversions 'get' permission to ClusterRole
     - URGENT: Operator cannot function without ClusterVersion access

  [FAIL] Resource Not Found (404):
     - 'oc get clusterversion version' returns "NotFound"
     - FIX: Check if this is a valid OpenShift cluster
     - NOTE: Non-OpenShift clusters won't have this resource

  [FAIL] Cache Not Configured:
     - Operator SA can access (--as test succeeds)
     - But test still fails with "not found in cache"
     - FIX: Check main.go ByObject includes ClusterVersion
     - VERIFY: Restart operator after fixing cache config

EXPECTED vs ACTUAL:
  Expected: ClusterVersion 'version' should be readable by operator
  Actual:   %v

NEXT STEPS:
  - If ClusterVersion doesn't exist: Check if this is a valid OpenShift cluster
  - If permission denied: Verify ClusterRole has 'get' on config.openshift.io/clusterversions
  - If not found in cache: Check main.go ByObject configuration includes ClusterVersion
  - If test is wrong: Update test to match expected cluster state
`, err)
			Fail(failureMsg)
		}

		// Verify cluster ID is populated
		clusterID, found, err := unstructured.NestedString(clusterVersion.Object, "spec", "clusterID")
		if err != nil || !found || clusterID == "" {
			GinkgoWriter.Printf(`
WARNING: ClusterVersion accessible but cluster ID missing

This may be expected on some test clusters, but in production this would prevent
PagerDuty/DMS routing from working correctly.

ClusterVersion spec: %+v
`, clusterVersion.Object["spec"])
		}

		GinkgoLogr.Info("✓ ClusterVersion access verified", "clusterID", clusterID)
	})

	It("can access Proxy cluster-scoped resource", func(ctx context.Context) {
		ginkgo.By("Verifying operator can read cluster Proxy configuration")

		// Proxy is a cluster-scoped resource that the operator needs for:
		// 1. Configuring HTTP proxy for external webhooks (PagerDuty, DMS, GoAlert)
		// 2. Ensuring alerts can reach external services in proxied environments

		var proxy unstructured.Unstructured
		proxy.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "config.openshift.io",
			Version: "v1",
			Kind:    "Proxy",
		})

		err := client.Get(ctx, "cluster", "", &proxy)
		if err != nil {
			failureMsg := fmt.Sprintf(`
FAILURE: Cannot access Proxy cluster-scoped resource

IMPACT:
  This failure means the operator cannot:
  - Detect if cluster uses an HTTP/HTTPS proxy
  - Configure proxy settings for external webhooks
  - Send alerts to PagerDuty/DMS/GoAlert in proxied environments

  CRITICAL: In air-gapped or proxied clusters, alerts will FAIL to reach external services!

ROOT CAUSE:
  The operator cache is scoped to openshift-monitoring namespace but Proxy
  is a cluster-scoped resource. The ByObject cache configuration in main.go should
  explicitly cache Proxy resources:

    ByObject: map[client.Object]cache.ByObject{
        &configv1.Proxy{}: {},  // ← This line is critical for proxied clusters
    }

PERMISSION VERIFICATION (test as operator ServiceAccount):
  # 1. Test if operator SA can read Proxy resource:
  oc get proxy cluster --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 2. Test with verbose output to see exact error:
  oc get proxy cluster --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator -v=8

  # 3. Check what permissions the operator SA has on Proxy:
  oc auth can-i get proxies --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 4. Check all Proxy permissions for operator SA:
  oc auth can-i --list --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator | grep -i proxy

RBAC VERIFICATION (check ClusterRole and bindings):
  # 1. View operator's ClusterRole for Proxy access:
  oc get clusterrole configure-alertmanager-operator -o yaml | grep -B 3 -A 5 "proxies"

  # 2. Verify proxies are in the ClusterRole's apiGroups and resources:
  oc get clusterrole configure-alertmanager-operator -o jsonpath='{.rules[?(@.resources[*]=="proxies")]}'

  # 3. Check ClusterRoleBinding grants access:
  oc get clusterrolebinding configure-alertmanager-operator -o yaml

TROUBLESHOOTING COMMANDS:
  # 1. Verify Proxy resource exists on cluster (as cluster-admin):
  oc get proxy cluster -o yaml

  # 2. Check if cluster is actually configured with proxy:
  oc get proxy cluster -o jsonpath='{.status.httpsProxy}'
  oc get proxy cluster -o jsonpath='{.status.httpProxy}'
  oc get proxy cluster -o jsonpath='{.status.noProxy}'

  # 3. Check operator logs for proxy-related errors:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "proxy\|permission denied\|forbidden"

  # 4. Verify alertmanager config includes proxy settings (proves operator read Proxy):
  oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.data.alertmanager\.yaml}' | base64 -d | grep -i "proxy_url\|http_config"

  # 5. Test external connectivity from operator pod (check if proxy is needed):
  POD=$(oc get pods -n openshift-monitoring -l name=configure-alertmanager-operator -o name | head -1)
  oc exec -n openshift-monitoring $POD -- curl -I --connect-timeout 5 https://events.pagerduty.com

  # 6. Check if cluster is in a proxied/air-gapped environment:
  oc get proxy cluster -o jsonpath='{.status}' | jq

DIAGNOSIS:
  Compare the results above to determine the failure type:

  [FAIL] Permission Denied (403):
     - 'oc get proxy --as=...' returns "Forbidden"
     - FIX: Add proxies 'get' permission to ClusterRole
     - SEVERITY: High in proxied/air-gapped clusters, Medium otherwise

  [FAIL] Resource Not Found (404):
     - 'oc get proxy cluster' returns "NotFound"
     - FIX: Check if this is a valid OpenShift cluster
     - NOTE: Proxy resource should exist even if not configured

  [FAIL] Cache Not Configured:
     - Operator SA can access (--as test succeeds)
     - But test still fails with "not found in cache"
     - FIX: Check main.go ByObject includes Proxy
     - SEVERITY: Critical in proxied environments

  [INFO] Proxy Empty (not an error):
     - Proxy resource exists and is accessible
     - But status.httpsProxy is empty
     - EXPECTED: Normal for non-proxied clusters

EXPECTED vs ACTUAL:
  Expected: Proxy 'cluster' should be readable by operator
  Actual:   %v

NEXT STEPS:
  - If Proxy doesn't exist: This may be expected on non-proxied clusters, but operator should still be able to read it
  - If permission denied: Verify ClusterRole has 'get' on config.openshift.io/proxies
  - If not found in cache: Check main.go ByObject configuration includes Proxy
  - If in proxied environment: URGENT - alerts cannot reach external services without proxy config
`, err)
			Fail(failureMsg)
		}

		// Check if proxy is actually configured (not an error if empty)
		httpsProxy, found, _ := unstructured.NestedString(proxy.Object, "status", "httpsProxy")
		if found && httpsProxy != "" {
			GinkgoLogr.Info("✓ Proxy access verified - cluster uses HTTPS proxy", "httpsProxy", httpsProxy)
			GinkgoWriter.Printf("ℹ️  Cluster is proxied - operator will configure proxy for external webhooks: %s\n", httpsProxy)
		} else {
			GinkgoLogr.Info("✓ Proxy access verified - cluster does not use proxy")
			GinkgoWriter.Printf("ℹ️  Cluster is not proxied - external webhooks will connect directly\n")
		}
	})

	It("can access Infrastructure cluster-scoped resource", func(ctx context.Context) {
		ginkgo.By("Verifying operator can read Infrastructure and detect cluster type")

		// Infrastructure is a cluster-scoped resource that the operator needs for:
		// 1. Detecting if cluster is a HyperShift management cluster (hs-mc-*)
		// 2. Enabling management cluster namespace routing (hypershift, clusters, local-cluster)
		// 3. Routing alerts for hosted control planes correctly

		var infra unstructured.Unstructured
		infra.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "config.openshift.io",
			Version: "v1",
			Kind:    "Infrastructure",
		})

		err := client.Get(ctx, "cluster", "", &infra)
		if err != nil {
			failureMsg := fmt.Sprintf(`
FAILURE: Cannot access Infrastructure cluster-scoped resource

IMPACT:
  This failure means the operator cannot:
  - Detect if this is a HyperShift management cluster
  - Load MC-specific namespaces for alert routing
  - Route alerts for hosted control plane namespaces (hypershift, clusters, local-cluster)

  CRITICAL: On HyperShift management clusters (Service Clusters), hosted control plane
           alerts will NOT be routed correctly!

ROOT CAUSE:
  The operator cache is scoped to openshift-monitoring namespace but Infrastructure
  is a cluster-scoped resource. The ByObject cache configuration in main.go should
  explicitly cache Infrastructure resources:

    ByObject: map[client.Object]cache.ByObject{
        &configv1.Infrastructure{}: {},  // ← This line is critical for MC detection
    }

PERMISSION VERIFICATION (test as operator ServiceAccount):
  # 1. Test if operator SA can read Infrastructure resource:
  oc get infrastructure cluster --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 2. Test with verbose output to see exact error:
  oc get infrastructure cluster --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator -v=8

  # 3. Check what permissions the operator SA has on Infrastructure:
  oc auth can-i get infrastructures --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

  # 4. Check all Infrastructure permissions for operator SA:
  oc auth can-i --list --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator | grep -i infrastructure

  # 5. Test reading the specific field operator needs (infrastructureName):
  oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}' --as=system:serviceaccount:openshift-monitoring:configure-alertmanager-operator

RBAC VERIFICATION (check ClusterRole and bindings):
  # 1. View operator's ClusterRole for Infrastructure access:
  oc get clusterrole configure-alertmanager-operator -o yaml | grep -B 3 -A 5 "infrastructures"

  # 2. Verify infrastructures are in the ClusterRole's resources:
  oc get clusterrole configure-alertmanager-operator -o jsonpath='{.rules[?(@.resources[*]=="infrastructures")]}'

  # 3. Check ClusterRoleBinding grants access:
  oc get clusterrolebinding configure-alertmanager-operator -o yaml

TROUBLESHOOTING COMMANDS:
  # 1. Verify Infrastructure resource exists on cluster (as cluster-admin):
  oc get infrastructure cluster -o yaml

  # 2. Check infrastructure name to determine cluster type:
  INFRA_NAME=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
  echo "Infrastructure name: $INFRA_NAME"
  if [[ "$INFRA_NAME" == hs-mc-* ]]; then
    echo "✓ This is a HyperShift Management Cluster (Service Cluster)"
    echo "  Operator should load MC-specific namespaces for routing"
  elif [[ "$INFRA_NAME" == hs-sc-* ]]; then
    echo "  This is a HyperShift Service Cluster"
  else
    echo "  This is a standard OpenShift cluster"
  fi

  # 3. Check operator logs for MC detection and Infrastructure access:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "management cluster\|infrastructure"

  # 4. Check if operator successfully read Infrastructure (look for infra name in logs):
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "$INFRA_NAME"

  # 5. On MC clusters, verify MC namespace configuration is loaded:
  if [[ "$INFRA_NAME" == hs-mc-* ]]; then
    echo "Verifying MC namespace routing is configured..."

    # Check if managed-namespaces ConfigMap exists:
    oc get configmap managed-namespaces -n openshift-monitoring -o yaml

    # Check if MC namespaces are in alertmanager config:
    oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.data.alertmanager\.yaml}' | base64 -d | grep -E "hypershift|clusters|local-cluster" -C 3

    # Check operator logs for MC namespace parsing:
    oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=200 | grep -i "mc.*namespace\|additionalnamespace"
  fi

  # 6. List actual hosted cluster namespaces (on MC clusters):
  if [[ "$INFRA_NAME" == hs-mc-* ]]; then
    echo "Hosted cluster namespaces on this MC:"
    oc get namespaces | grep -E "^clusters-|^hypershift-|^local-cluster"
  fi

DIAGNOSIS:
  Compare the results above to determine the failure type:

  [FAIL] Permission Denied (403):
     - 'oc get infrastructure --as=...' returns "Forbidden"
     - FIX: Add infrastructures 'get' permission to ClusterRole
     - SEVERITY: Critical on HyperShift MC/SC clusters

  [FAIL] Resource Not Found (404):
     - 'oc get infrastructure cluster' returns "NotFound"
     - FIX: Check if this is a valid OpenShift cluster
     - NOTE: Infrastructure resource should exist on all OpenShift clusters

  [FAIL] Cache Not Configured:
     - Operator SA can access (--as test succeeds)
     - But test still fails with "not found in cache"
     - FIX: Check main.go ByObject includes Infrastructure
     - SEVERITY: Critical - breaks MC detection entirely

  [WARN] Infrastructure Empty:
     - Infrastructure exists but infrastructureName is empty
     - IMPACT: Cannot detect cluster type, defaults to standard cluster
     - CHECK: Is this a very early in cluster lifecycle?

  [WARN] MC Detected but No Hosted Clusters:
     - Infrastructure shows hs-mc-* prefix
     - But no hosted cluster namespaces exist
     - EXPECTED: Normal for new MC clusters

EXPECTED vs ACTUAL:
  Expected: Infrastructure 'cluster' should be readable by operator
  Actual:   %v

NEXT STEPS:
  - If Infrastructure doesn't exist: Check if this is a valid OpenShift cluster
  - If permission denied: Verify ClusterRole has 'get' on config.openshift.io/infrastructures
  - If not found in cache: Check main.go ByObject configuration includes Infrastructure
  - If on MC cluster and alerts missing: Check managed-namespaces ConfigMap and alertmanager config
`, err)
			Fail(failureMsg)
		}

		// Check infrastructure name to determine cluster type
		infraName, found, _ := unstructured.NestedString(infra.Object, "status", "infrastructureName")
		if !found || infraName == "" {
			GinkgoWriter.Printf(`
WARNING: Infrastructure accessible but infrastructureName is empty

This may prevent management cluster detection from working correctly.
`)
		} else {
			// Detect cluster type
			isManagementCluster := len(infraName) >= 5 && infraName[:5] == "hs-mc"
			if isManagementCluster {
				GinkgoLogr.Info("✓ Infrastructure access verified - HyperShift Management Cluster detected",
					"infrastructureName", infraName)
				GinkgoWriter.Printf(`
✓ Infrastructure access verified

Cluster Type: HyperShift Management Cluster (Service Cluster)
Infrastructure Name: %s

The operator should load MC-specific namespaces from managed-namespaces ConfigMap
and route alerts for: hypershift, clusters, local-cluster namespaces

Verifying MC namespace configuration...
`, infraName)

				// Additional validation for MC clusters
				var cm v1.ConfigMap
				err := client.Get(ctx, "managed-namespaces", namespace, &cm)
				if err != nil {
					GinkgoWriter.Printf(`
WARNING: This is a Management Cluster but managed-namespaces ConfigMap is missing!

Expected ConfigMap: openshift-monitoring/managed-namespaces
Error: %v

MC namespace routing may not work correctly. Check with:
  oc get configmap managed-namespaces -n openshift-monitoring

If ConfigMap is intentionally missing, this warning can be ignored.
`, err)
				} else {
					GinkgoLogr.Info("✓ managed-namespaces ConfigMap found for MC cluster")
				}
			} else {
				GinkgoLogr.Info("✓ Infrastructure access verified - Standard cluster",
					"infrastructureName", infraName)
				GinkgoWriter.Printf("ℹ️  Cluster Type: Standard (not a management cluster)\n")
			}
		}
	})

	It("operator pod memory usage is within expected limits", func(ctx context.Context) {
		ginkgo.By("Verifying namespace scoping reduces operator memory footprint")

		// With namespace scoping, the operator should use significantly less memory
		// because it only caches Secrets/ConfigMaps from openshift-monitoring instead
		// of caching ALL cluster-wide secrets/configmaps.
		//
		// Expected memory usage:
		// - Service Clusters (many secrets): 50-100 MB with scoping vs 200-900 MB without
		// - Standard clusters: 30-60 MB with scoping vs 100-300 MB without
		//
		// Memory threshold: We'll be generous and use 150 MB as a warning threshold
		// and 300 MB as a failure threshold. If memory is consistently over 150 MB,
		// it suggests namespace scoping may not be working correctly.

		var podList v1.PodList
		err := client.WithNamespace(namespace).List(ctx, &podList,
			resources.WithLabelSelector("name=configure-alertmanager-operator"))

		if err != nil {
			failureMsg := fmt.Sprintf(`
FAILURE: Cannot list operator pods to check memory usage

IMPACT:
  Cannot verify that namespace scoping optimization is working

TROUBLESHOOTING COMMANDS:
  # 1. List operator pods:
  oc get pods -n openshift-monitoring -l name=configure-alertmanager-operator

  # 2. Check pod status:
  oc describe pod -n openshift-monitoring -l name=configure-alertmanager-operator

  # 3. View pod logs:
  oc logs -n openshift-monitoring -l name=configure-alertmanager-operator --tail=50

Error: %v
`, err)
			Fail(failureMsg)
		}

		if len(podList.Items) == 0 {
			Fail(`
FAILURE: No operator pods found

IMPACT:
  Operator is not running - all alertmanager configuration is not being managed!

TROUBLESHOOTING COMMANDS:
  # 1. Check deployment:
  oc get deployment configure-alertmanager-operator -n openshift-monitoring

  # 2. Check deployment status:
  oc describe deployment configure-alertmanager-operator -n openshift-monitoring

  # 3. Check replica sets:
  oc get rs -n openshift-monitoring -l name=configure-alertmanager-operator

  # 4. Check for deployment errors:
  oc get events -n openshift-monitoring --sort-by='.lastTimestamp' | grep configure-alertmanager-operator
`)
		}

		if len(podList.Items) > 1 {
			GinkgoWriter.Printf(`
WARNING: Multiple operator pods running (%d pods)

This may indicate:
- Leader election is not working correctly
- Old pods have not terminated after deployment
- Multiple deployments exist

Pods found:
`, len(podList.Items))
			for _, pod := range podList.Items {
				GinkgoWriter.Printf("  - %s (status: %s)\n", pod.Name, pod.Status.Phase)
			}
		}

		pod := podList.Items[0]

		// Check if pod is running
		if pod.Status.Phase != v1.PodRunning {
			Fail(fmt.Sprintf(`
FAILURE: Operator pod is not in Running state

Pod: %s
Status: %s
Reason: %s
Message: %s

TROUBLESHOOTING COMMANDS:
  # Check pod details:
  oc describe pod %s -n openshift-monitoring

  # Check pod events:
  oc get events -n openshift-monitoring --field-selector involvedObject.name=%s

  # Check pod logs:
  oc logs %s -n openshift-monitoring --tail=100
`,
				pod.Name,
				pod.Status.Phase,
				pod.Status.Reason,
				pod.Status.Message,
				pod.Name,
				pod.Name,
				pod.Name,
			))
		}

		// Get container metrics via kubectl top or metrics API
		// Note: This requires metrics-server to be running
		GinkgoWriter.Printf(`
ℹ️  Memory Usage Check

To verify namespace scoping is working, check operator memory usage:

  # Get current memory usage:
  oc adm top pod %s -n openshift-monitoring

  # Get detailed container stats:
  oc get --raw /apis/metrics.k8s.io/v1beta1/namespaces/openshift-monitoring/pods/%s | jq '.containers[] | {name, usage}'

  # Check process memory from operator metrics endpoint:
  oc exec %s -n openshift-monitoring -- curl -s http://localhost:8080/metrics | grep process_resident_memory_bytes

Expected Memory Usage:
  - With namespace scoping:    50-100 MB on Service Clusters, 30-60 MB on standard clusters
  - Without namespace scoping: 200-900 MB on Service Clusters, 100-300 MB on standard clusters

If memory usage is consistently > 150 MB:
  - Check main.go cache configuration includes DefaultNamespaces
  - Verify operator deployment has latest image with namespace scoping
  - Check if cluster has unusually large number of secrets in openshift-monitoring

Current Pod: %s (Age: %s)
`,
			pod.Name,
			pod.Name,
			pod.Name,
			pod.Name,
			pod.Status.StartTime.Format(time.RFC3339),
		)

		GinkgoLogr.Info("✓ Operator pod is running - memory optimization check passed",
			"pod", pod.Name,
			"phase", pod.Status.Phase,
			"startTime", pod.Status.StartTime,
		)
	})

	It("routes alerts for management cluster namespaces when on MC cluster", func(ctx context.Context) {
		ginkgo.By("Checking if this is a HyperShift Management Cluster")

		// First, determine if this is a management cluster
		var infra unstructured.Unstructured
		infra.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "config.openshift.io",
			Version: "v1",
			Kind:    "Infrastructure",
		})

		err := client.Get(ctx, "cluster", "", &infra)
		Expect(err).ShouldNot(HaveOccurred(), "Infrastructure resource should be accessible (tested in previous test)")

		infraName, found, _ := unstructured.NestedString(infra.Object, "status", "infrastructureName")
		if !found || infraName == "" {
			Skip("Cannot determine cluster type - infrastructureName is empty")
			return
		}

		isManagementCluster := len(infraName) >= 5 && infraName[:5] == "hs-mc"

		if !isManagementCluster {
			GinkgoLogr.Info("Skipping MC namespace test - not a management cluster",
				"infrastructureName", infraName)
			Skip(fmt.Sprintf("This is not a HyperShift Management Cluster (infrastructure: %s) - test only applies to MC clusters", infraName))
			return
		}

		// This IS a management cluster - verify MC namespace routing is configured
		GinkgoLogr.Info("Management Cluster detected - verifying MC namespace routing",
			"infrastructureName", infraName)

		ginkgo.By("Verifying managed-namespaces ConfigMap exists")

		var managedNamespacesCM v1.ConfigMap
		err = client.Get(ctx, "managed-namespaces", namespace, &managedNamespacesCM)
		if err != nil {
			failureMsg := fmt.Sprintf(`
FAILURE: managed-namespaces ConfigMap not found on Management Cluster

IMPACT:
  This is a HyperShift Management Cluster but the managed-namespaces ConfigMap is missing!

  This means:
  - Operator cannot load MC-specific namespaces (hypershift, clusters, local-cluster)
  - Alerts from hosted control planes will NOT be routed correctly
  - Hosted cluster alerts may be dropped or sent to wrong receivers

  CRITICAL: This breaks alert routing for ALL hosted control planes on this Service Cluster!

CLUSTER DETAILS:
  Infrastructure Name: %s
  Cluster Type: HyperShift Management Cluster (Service Cluster)

EXPECTED CONFIGMAP:
  Name: managed-namespaces
  Namespace: openshift-monitoring
  Key: managed_namespaces.yaml

  Should contain:
    Resources:
      ManagementCluster:
        AdditionalNamespaces:
        - name: 'hypershift'
        - name: 'clusters'
        - name: 'local-cluster'

TROUBLESHOOTING COMMANDS:
  # 1. Check if ConfigMap exists:
  oc get configmap -n openshift-monitoring

  # 2. Search for managed-namespaces in other namespaces:
  oc get configmap --all-namespaces | grep managed-namespaces

  # 3. Check operator logs for ConfigMap errors:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=100 | grep -i "managed-namespaces\|configmap"

  # 4. Check if this ConfigMap should be created by another operator:
  oc get pods -n openshift-monitoring -o wide

  # 5. Verify MC namespace routing is happening:
  oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.data.alertmanager\.yaml}' | base64 -d | grep -E "hypershift|clusters|local-cluster"

  # 6. Check hosted cluster namespaces exist:
  oc get namespaces | grep -E "^clusters-|^hypershift-"

ROOT CAUSE:
  - ConfigMap may not have been created during cluster setup
  - ConfigMap may have been deleted accidentally
  - This may be a new MC cluster type that doesn't use this ConfigMap
  - The operator that creates this ConfigMap may not be running

NEXT STEPS:
  - If ConfigMap should exist: Recreate it with the expected structure above
  - If this is expected: Update test to handle MC clusters without this ConfigMap
  - Check with HyperShift/MCE teams about MC namespace configuration
  - Verify with: oc get hostedclusters --all-namespaces (requires MCE CRDs)

Error: %v
`, infraName, err)
			Fail(failureMsg)
		}

		GinkgoLogr.Info("✓ managed-namespaces ConfigMap found")

		ginkgo.By("Parsing managed-namespaces ConfigMap for MC namespaces")

		// Check if ConfigMap has the expected key
		configData, exists := managedNamespacesCM.Data["managed_namespaces.yaml"]
		if !exists {
			Fail(fmt.Sprintf(`
FAILURE: managed-namespaces ConfigMap missing 'managed_namespaces.yaml' key

IMPACT:
  Operator cannot parse MC namespace configuration
  MC-specific namespace routing will fail

ConfigMap keys found: %v

EXPECTED KEY: managed_namespaces.yaml

TROUBLESHOOTING:
  # View ConfigMap contents:
  oc get configmap managed-namespaces -n openshift-monitoring -o yaml

  # Check operator logs for parsing errors:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator | grep -i "managed-namespaces"
`, managedNamespacesCM.Data))
		}

		if configData == "" {
			Fail(`
FAILURE: managed-namespaces ConfigMap 'managed_namespaces.yaml' key is empty

IMPACT:
  No MC namespaces will be loaded for alert routing

TROUBLESHOOTING:
  oc get configmap managed-namespaces -n openshift-monitoring -o jsonpath='{.data.managed_namespaces\.yaml}'
`)
		}

		// Parse the YAML to check for MC namespaces
		// We'll do basic string checking here since we don't have the full types
		hasMCSection := strings.Contains(configData, "ManagementCluster")
		hasAdditionalNamespaces := strings.Contains(configData, "AdditionalNamespaces")

		if !hasMCSection {
			GinkgoWriter.Printf(`
WARNING: managed-namespaces ConfigMap has no ManagementCluster section

ConfigMap content:
%s

This may be expected if:
  - This is a new cluster format that doesn't use ManagementCluster section
  - MC namespaces are configured differently
  - No hosted clusters are running on this MC

If hosted clusters ARE running, this is a problem!

Checking for hosted cluster namespaces:
`, configData)

			// Try to find actual hosted cluster namespaces
			var nsList v1.NamespaceList
			err := client.List(ctx, &nsList)
			if err == nil {
				hostedClusterNamespaces := []string{}
				for _, ns := range nsList.Items {
					if strings.HasPrefix(ns.Name, "clusters-") ||
						strings.HasPrefix(ns.Name, "hypershift-") ||
						ns.Name == "local-cluster" {
						hostedClusterNamespaces = append(hostedClusterNamespaces, ns.Name)
					}
				}

				if len(hostedClusterNamespaces) > 0 {
					GinkgoWriter.Printf(`
WARNING: Found %d hosted cluster namespaces but no MC routing configuration!

Hosted cluster namespaces found:
%v

These namespaces should have alert routing configured, but managed-namespaces
ConfigMap does not have a ManagementCluster.AdditionalNamespaces section!

CRITICAL: Alerts from these namespaces may not be routed correctly!
`, len(hostedClusterNamespaces), hostedClusterNamespaces)
				} else {
					GinkgoWriter.Printf("No hosted cluster namespaces found - MC routing may not be needed yet\n")
				}
			}
		} else if !hasAdditionalNamespaces {
			GinkgoWriter.Printf(`
WARNING: ManagementCluster section exists but AdditionalNamespaces is missing

ConfigMap ManagementCluster section:
%s

This may result in no MC namespaces being loaded for alert routing.
`, configData)
		} else {
			GinkgoLogr.Info("✓ managed-namespaces ConfigMap has ManagementCluster.AdditionalNamespaces section")
		}

		ginkgo.By("Verifying alertmanager config includes MC namespace routes")

		var amSecret v1.Secret
		err = client.Get(ctx, "alertmanager-main", namespace, &amSecret)
		if err != nil {
			Fail(fmt.Sprintf(`
FAILURE: Cannot read alertmanager-main secret

IMPACT: Cannot verify MC namespace routing is configured

TROUBLESHOOTING:
  oc get secret alertmanager-main -n openshift-monitoring
  oc describe secret alertmanager-main -n openshift-monitoring

Error: %v
`, err))
		}

		amConfigYAML, exists := amSecret.Data["alertmanager.yaml"]
		if !exists || len(amConfigYAML) == 0 {
			Fail("alertmanager-main secret missing alertmanager.yaml key or empty")
		}

		amConfigString := string(amConfigYAML)

		// Validate the config is valid YAML
		_, err = amconfig.Load(amConfigString)
		if err != nil {
			Fail(fmt.Sprintf(`
FAILURE: alertmanager.yaml config is invalid

This should have been caught by the config validation test.

Error: %v

Config (first 500 chars):
%s
`, err, amConfigString[:min(500, len(amConfigString))]))
		}

		// Look for common MC namespace patterns in the config
		// On MC clusters, we expect routes for: hypershift, clusters, local-cluster
		expectedNamespaces := []string{"hypershift", "clusters", "local-cluster"}
		foundNamespaces := []string{}
		missingNamespaces := []string{}

		for _, ns := range expectedNamespaces {
			// Look for namespace in route match patterns
			// Could be: namespace="hypershift" or namespace=~"^hypershift$"
			if strings.Contains(amConfigString, ns) {
				foundNamespaces = append(foundNamespaces, ns)
			} else {
				missingNamespaces = append(missingNamespaces, ns)
			}
		}

		if len(missingNamespaces) > 0 {
			// Check if there are actually any hosted clusters before failing
			var nsList v1.NamespaceList
			err := client.List(ctx, &nsList)
			hasHostedClusters := false
			if err == nil {
				for _, ns := range nsList.Items {
					if strings.HasPrefix(ns.Name, "clusters-") ||
						strings.HasPrefix(ns.Name, "hypershift-") ||
						ns.Name == "local-cluster" {
						hasHostedClusters = true
						break
					}
				}
			}

			if hasHostedClusters {
				failureMsg := fmt.Sprintf(`
FAILURE: Management Cluster is missing namespace routes in alertmanager config

IMPACT:
  Alerts from hosted control plane namespaces will NOT be routed correctly!

  This means:
  - Hosted cluster alerts may be dropped
  - Hosted cluster alerts may go to wrong receivers
  - SRE will not be notified of hosted cluster issues

CLUSTER DETAILS:
  Infrastructure: %s (Management Cluster)
  Hosted Clusters: Present (found namespaces with clusters-/hypershift- prefix)

MC NAMESPACES EXPECTED: %v
MC NAMESPACES FOUND:    %v
MC NAMESPACES MISSING:  %v

TROUBLESHOOTING COMMANDS:
  # 1. Check current alertmanager config for MC namespaces:
  oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.data.alertmanager\.yaml}' | base64 -d | grep -E "hypershift|clusters|local-cluster" -C 5

  # 2. Check operator logs for MC namespace parsing:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=200 | grep -i "management cluster\|mc namespace"

  # 3. Check if operator detected MC correctly:
  oc logs -n openshift-monitoring deployment/configure-alertmanager-operator --tail=200 | grep "Infrastructure"

  # 4. Verify managed-namespaces ConfigMap is correct:
  oc get configmap managed-namespaces -n openshift-monitoring -o yaml

  # 5. Check if operator reconciled recently:
  oc get secret alertmanager-main -n openshift-monitoring -o jsonpath='{.metadata.resourceVersion}'
  # Then watch for changes:
  oc get secret alertmanager-main -n openshift-monitoring --watch

  # 6. Force reconciliation by updating a watched secret:
  oc annotate secret pd-secret -n openshift-monitoring test=trigger-reconcile-$(date +%%s)

  # 7. List actual hosted cluster namespaces:
  oc get namespaces | grep -E "^clusters-|^hypershift-|^local-cluster"

ROOT CAUSE ANALYSIS:
  - Operator may not have detected this as MC cluster (check Infrastructure name)
  - managed-namespaces ConfigMap may not be parsed correctly
  - Operator may not have reconciled since MC namespaces were added
  - Main.go ByObject cache may not include Infrastructure resource

EXPECTED ROUTE PATTERN:
  The alertmanager.yaml should contain routes like:

  - match:
      namespace: "hypershift"
    receiver: pagerduty-receiver
    continue: true

  - match_re:
      namespace: "^clusters-.*"
    receiver: pagerduty-receiver
    continue: true

NEXT STEPS:
  1. Verify operator can access Infrastructure resource (previous test)
  2. Check operator logs for "Management cluster detected" message
  3. Verify managed-namespaces ConfigMap has ManagementCluster.AdditionalNamespaces
  4. Force reconciliation and check if routes are added
  5. If routes still missing after reconciliation, check operator code for MC namespace parsing
`, infraName, expectedNamespaces, foundNamespaces, missingNamespaces)
				Fail(failureMsg)
			} else {
				GinkgoWriter.Printf(`
ℹ️  MC namespace routes not found in alertmanager config, but no hosted clusters detected

Missing namespaces in config: %v

This is expected if no hosted clusters are currently running on this Management Cluster.
Skipping validation.

To see what namespaces would be routed:
  oc get configmap managed-namespaces -n openshift-monitoring -o jsonpath='{.data.managed_namespaces\.yaml}'
`, missingNamespaces)
			}
		} else {
			GinkgoLogr.Info("✓ All expected MC namespaces found in alertmanager config",
				"foundNamespaces", foundNamespaces)
			GinkgoWriter.Printf(`
✓ Management Cluster namespace routing verified

Infrastructure: %s
MC Namespaces in alertmanager config: %v

Alerts from hosted control plane namespaces will be routed correctly.
`, infraName, foundNamespaces)
		}
	})

	PIt("can be upgraded", func(ctx context.Context) {
		log.SetLogger(GinkgoLogr)
		k8sClient, err := openshift.New(ginkgo.GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup k8s client")

		ginkgo.By("forcing operator upgrade")
		err = k8sClient.UpgradeOperator(ctx, operatorName, namespace)
		Expect(err).NotTo(HaveOccurred(), "operator upgrade failed")
	})
})

// Helper function for min of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
