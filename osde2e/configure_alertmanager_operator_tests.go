// DO NOT REMOVE TAGS BELOW. IF ANY NEW TEST FILES ARE CREATED UNDER /osde2e, PLEASE ADD THESE TAGS TO THEM IN ORDER TO BE EXCLUDED FROM UNIT TESTS. //go:build osde2e
//go:build osde2e
// +build osde2e

package osde2etests

import (
	"context"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/osde2e-common/pkg/clients/openshift"
	appsv1 "k8s.io/api/apps/v1"
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
		secrets         = []string{"pd-secret", "dms-secret"}
		serviceAccounts = []string{"configure-alertmanager-operator"}
	)
	const (
		timeoutDuration   = 300 * time.Second
		pollingDuration   = 30 * time.Second
		configMapLockFile = "configure-alertmanager-operator-lock"
		namespace         = "openshift-monitoring"
		operatorName      = "configure-alertmanager-operator"
	)

	BeforeAll(func() {
		cfg, err := config.GetConfig()
		Expect(err).Should(BeNil(), "failed to get kubeconfig")
		client, err = resources.New(cfg)
		Expect(err).Should(BeNil(), "resources.New error")

		dynamicClient, err = dynamic.NewForConfig(cfg)
		Expect(err).ShouldNot(HaveOccurred(), "failed to configure Dynamic client")
	})

	It("cluster service version exists", func(ctx context.Context) {
		Eventually(func() bool {
			csvList, err := dynamicClient.Resource(
				schema.GroupVersionResource{
					Group:    "operators.coreos.com",
					Version:  "v1alpha1",
					Resource: "clusterserviceversions",
				},
			).Namespace(namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred(), "Failed to retrieve CSV from namespace %s", namespace)

			for _, csv := range csvList.Items {
				specName, _, _ := unstructured.NestedFieldCopy(csv.Object, "spec", "displayName")
				statusPhase, _, _ := unstructured.NestedFieldCopy(csv.Object, "status", "phase")
				if statusPhase == "Succeeded" && specName == operatorName {
					return true
				}
			}
			return false
		}).WithTimeout(timeoutDuration).WithPolling(pollingDuration).Should(BeTrue(), "CSV %s should exist and have Succeeded status", operatorName)
	})

	It("service accounts exist", func(ctx context.Context) {
		for _, serviceAccount := range serviceAccounts {
			err := client.Get(ctx, serviceAccount, namespace, &v1.ServiceAccount{})
			Expect(err).ShouldNot(HaveOccurred(), "Service account %s not found", serviceAccount)
		}
	})

	It("deployment exists", func(ctx context.Context) {
		if client == nil {
			Fail("Kubernetes client is not initialized")
		}

		cond := conditions.New(client)
		if cond == nil {
			Fail("Failed to create conditions object")
		}
		err := wait.For(conditions.New(client).DeploymentConditionMatch(
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      operatorName,
					Namespace: namespace,
				},
			}, appsv1.DeploymentAvailable, v1.ConditionTrue))
		Expect(err).ShouldNot(HaveOccurred(), "Deployment %s not available", operatorName)
	})

	It("roles exist", func(ctx context.Context) {
		var roles rbacv1.RoleList
		err := client.WithNamespace(namespace).List(ctx, &roles)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get roles")
		found := false
		for _, role := range roles.Items {
			if strings.HasPrefix(role.Name, operatorName) {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "Roles not found")
	})

	It("role bindings exist", func(ctx context.Context) {
		var roleBindings rbacv1.RoleBindingList
		err := client.List(ctx, &roleBindings)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get role bindings")
		found := false
		for _, roleBinding := range roleBindings.Items {
			if strings.HasPrefix(roleBinding.Name, operatorName) {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "Role bindings not found")
	})

	It("cluster roles exist", func(ctx context.Context) {
		var clusterRoles rbacv1.ClusterRoleList
		err := client.WithNamespace(namespace).List(ctx, &clusterRoles)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get cluster roles")
		found := false
		for _, clusterRole := range clusterRoles.Items {
			olmOwner := clusterRole.Labels["olm.owner"]
			if strings.HasPrefix(olmOwner, operatorName) {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "Cluster roles not found")
	})

	It("cluster role bindings exist", func(ctx context.Context) {
		var clusterRoleBindings rbacv1.ClusterRoleBindingList
		err := client.List(ctx, &clusterRoleBindings)
		Expect(err).ShouldNot(HaveOccurred(), "Failed to get cluster role bindings")
		found := false
		for _, clusterRoleBinding := range clusterRoleBindings.Items {
			if strings.HasPrefix(clusterRoleBinding.Name, operatorName) {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "Cluster role bindings not found")
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

	It("can be upgraded", func(ctx context.Context) {
		log.SetLogger(GinkgoLogr)
		k8sClient, err := openshift.New(ginkgo.GinkgoLogr)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup k8s client")

		ginkgo.By("forcing operator upgrade")
		err = k8sClient.UpgradeOperator(ctx, operatorName, namespace)
		Expect(err).NotTo(HaveOccurred(), "operator upgrade failed")
	})
})
