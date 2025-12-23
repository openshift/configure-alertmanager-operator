// DO NOT REMOVE TAGS BELOW. IF ANY NEW TEST FILES ARE CREATED UNDER /osde2e, PLEASE ADD THESE TAGS TO THEM IN ORDER TO BE EXCLUDED FROM UNIT TESTS. //go:build osde2e
//go:build osde2e
// +build osde2e

package osde2etests

import (
	"context"
	"fmt"
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
		// Query the alertmanager_config_validation_status metric
		// Metric value: 0 = valid config, 1 = invalid config
		query := `alertmanager_config_validation_status{name="configure-alertmanager-operator"}`

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

	PIt("can be upgraded", func(ctx context.Context) {
		log.SetLogger(GinkgoLogr)
		k8sClient, err := openshift.New(ginkgo.GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup k8s client")

		ginkgo.By("forcing operator upgrade")
		err = k8sClient.UpgradeOperator(ctx, operatorName, namespace)
		Expect(err).NotTo(HaveOccurred(), "operator upgrade failed")
	})
})
