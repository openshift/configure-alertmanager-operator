// DO NOT REMOVE TAGS BELOW. IF ANY NEW TEST FILES ARE CREATED UNDER /osde2e, PLEASE ADD THESE TAGS TO THEM IN ORDER TO BE EXCLUDED FROM UNIT TESTS.
//go:build osde2e
// +build osde2e

package osde2etests

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/osde2e-common/pkg/clients/openshift"
	"github.com/openshift/osde2e-common/pkg/clients/prometheus"
	. "github.com/openshift/osde2e-common/pkg/gomega/assertions"
	. "github.com/openshift/osde2e-common/pkg/gomega/matchers"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

var _ = ginkgo.Describe("configure-alertmanager-operator", ginkgo.Ordered, func() {
	var (
		k8s  *openshift.Client
		prom *prometheus.Client
	)

	var (
		namespaceName     = "openshift-monitoring"
		operatorName      = "configure-alertmanager-operator"
		serviceAccount    = "configure-alertmanager-operator"
		rolePrefix        = operatorName
		clusterRolePrefix = operatorName
		secrets           = []string{"pd-secret", "dms-secret"}
		configMaps        = []string{"managed-namespaces", "ocp-namespaces", "ocm-agent", "configure-alertmanager-operator-lock"}
	)

	ginkgo.BeforeAll(func(ctx context.Context) {
		var err error
		k8s, err = openshift.New()
		Expect(err).ShouldNot(HaveOccurred(), "unable to create k8s client")

		prom, err = prometheus.New(ctx, k8s)
		Expect(err).ShouldNot(HaveOccurred(), "unable to setup prometheus client")
	})

	ginkgo.It("is installed", func(ctx context.Context) {
		ginkgo.By("checking the serviceaccount exists")
		err := k8s.Get(ctx, serviceAccount, namespaceName, &corev1.ServiceAccount{})
		Expect(err).ShouldNot(HaveOccurred(), "service account %s not found", serviceAccount)

		ginkgo.By("checking the roles exists")
		var roles rbacv1.RoleList
		err = k8s.List(ctx, &roles)
		Expect(err).ShouldNot(HaveOccurred(), "failed to get roles")
		Expect(&roles).Should(ContainItemWithPrefix(rolePrefix), "unable to find roles with prefix %s", rolePrefix)

		ginkgo.By("checking the rolebindings exist")
		var roleBindings rbacv1.RoleBindingList
		err = k8s.List(ctx, &roleBindings)
		Expect(err).ShouldNot(HaveOccurred(), "failed to get role bindings")
		Expect(&roleBindings).Should(ContainItemWithPrefix(rolePrefix), "unable to find rolebindings with prefix %s", rolePrefix)

		ginkgo.By("checking the clusterrole exists")
		var clusterRoles rbacv1.ClusterRoleList
		err = k8s.List(ctx, &clusterRoles)
		Expect(err).ShouldNot(HaveOccurred(), "failed to get cluster roles")
		Expect(&clusterRoles).Should(ContainItemWithPrefix(clusterRolePrefix), "unable to find cluster role with prefix %s", clusterRolePrefix)

		ginkgo.By("checking the clusterrolebinding exists")
		var clusterRoleBindings rbacv1.ClusterRoleBindingList
		err = k8s.List(ctx, &clusterRoleBindings)
		Expect(err).ShouldNot(HaveOccurred(), "failed to get cluster role bindings")
		Expect(&clusterRoleBindings).Should(ContainItemWithPrefix(clusterRolePrefix), "unable to find clusterrolebinding with prefix %s", clusterRolePrefix)

		ginkgo.By("checking the required configmaps exist")
		for _, configMap := range configMaps {
			ginkgo.By(fmt.Sprintf("checking the configmap %s exists", configMap))
			err = k8s.Get(ctx, configMap, namespaceName, &corev1.ConfigMap{})
			Expect(err).ShouldNot(HaveOccurred(), "failed to get config map %s", configMap)
		}

		ginkgo.By("checking the deployment is available")
		EventuallyDeployment(ctx, k8s, operatorName, namespaceName).Should(BeAvailable())

		ginkgo.By("checking the operator is publishing metrics to prometheus")
		results, err := prom.InstantQuery(ctx, `up{job="configure-alertmanager-operator"}`)
		Expect(err).ShouldNot(HaveOccurred(), "failed to query prometheus")
		result := results[0].Value
		Expect(int(result)).Should(BeNumerically("==", 1), "prometheus exporter is not healthy")

		var clusterVersion configv1.ClusterVersion
		err = k8s.Get(ctx, "version", "", &clusterVersion)
		Expect(err).ShouldNot(HaveOccurred(), "failed to get clusterversion")

		// Do not check secrets on nightly clusters
		// https://github.com/openshift/pagerduty-operator/blob/master/hack/olm-registry/olm-artifacts-template.yaml
		if !strings.HasPrefix(clusterVersion.Spec.Channel, "nightly") {
			for _, secret := range secrets {
				err = k8s.Get(ctx, secret, namespaceName, &corev1.Secret{})
				Expect(err).ShouldNot(HaveOccurred(), "secret %s not found", secret)
			}
		}
	})

	// TODO: ginkgo.It("can be upgraded", ...)
})
