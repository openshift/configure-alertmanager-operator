/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/operator-framework/operator-lib/leader"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	configv1 "github.com/openshift/api/config/v1"
	operatorconfig "github.com/openshift/configure-alertmanager-operator/config"
	operatormetrics "github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/openshift/configure-alertmanager-operator/controllers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme            = k8sruntime.NewScheme()
	setupLog          = ctrl.Log.WithName("setup")
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.Install(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		//MetricsBindAddress:     fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		Metrics: metricsserver.Options{BindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort)},
		//Port:                   9443,
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,

		// Cache scoping: Only cache namespaced resources from openshift-monitoring
		// This reduces memory usage by ~90% (910 MB -> 90 MB on Service Clusters)
		// by avoiding caching of all cluster-wide secrets/configmaps.
		// See NAMESPACE_SCOPING_SAFETY_ANALYSIS.md for detailed analysis.
		Cache: cache.Options{
			// Only cache namespaced resources (Secrets, ConfigMaps) from openshift-monitoring
			DefaultNamespaces: map[string]cache.Config{
				operatorconfig.OperatorNamespace: {}, // "openshift-monitoring"
			},
			// Explicitly cache cluster-scoped resources the operator accesses
			ByObject: map[client.Object]cache.ByObject{
				// ClusterVersion: watched + accessed via Get() for cluster ID
				&configv1.ClusterVersion{}: {},
				// Proxy: accessed via Get() for HTTPS proxy settings (not watched)
				&configv1.Proxy{}: {},
				// Infrastructure: accessed via Get() for management cluster detection (not watched)
				&configv1.Infrastructure{}: {},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Print configuration info
	printVersion()
	if err := operatorconfig.SetIsFedramp(); err != nil {
		setupLog.Error(err, "failed to get fedramp value")
		os.Exit(1)
	}
	if operatorconfig.IsFedramp() {
		setupLog.Info("running in fedramp environment.")
	}

	// Leader election: Ensures only one active operator instance modifies the cluster
	// Set SKIP_LEADER_ELECTION=true ONLY for local testing/development with read-only kubeconfig.
	// Production deployments MUST use leader election to prevent concurrent writes.
	//
	// Why skip for local testing:
	// - leader.Become() requires write permission to create a ConfigMap lock
	// - Local testing with read-only ServiceAccount would fail at this step
	// - Allows running operator locally to profile memory usage without cluster write access
	//
	// WARNING: Never set SKIP_LEADER_ELECTION=true in production - it allows multiple
	// operator instances to run simultaneously and make conflicting changes.
	if os.Getenv("SKIP_LEADER_ELECTION") != "true" {
		err = leader.Become(context.TODO(), "configure-alertmanager-operator-lock")
		if err != nil {
			setupLog.Error(err, "Failed to retry for leader lock")
			os.Exit(1)
		}
	} else {
		setupLog.Info("Skipping leader election (SKIP_LEADER_ELECTION=true)")
	}

	if err = (&controllers.SecretReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Secret")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Create Service object to expose the metrics port.
	s, svcerr := operatormetrics.GenerateService(8080, "metrics")
	if svcerr != nil {
		log.Error(err, "Error generating metrics service object.")
		panic("Ensure that operator is running in a cluster and not in a local development environment.")
	} else {
		log.Info("Generated metrics service object")
	}

	sm := operatormetrics.GenerateServiceMonitor(s)
	err = mgr.GetClient().Create(context.TODO(), s)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		log.Error(err, "error creating metrics Service", "name", s.Name)
	} else {
		log.Info("metrics Service created or already exists", "name", s.Name)
		err = mgr.GetClient().Create(context.TODO(), sm)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			log.Error(err, "error creating metrics ServiceMonitor", "name", sm.Name)
		} else {
			log.Info("metrics ServiceMonitor created or already exists", "name", sm.Name)
		}
	}

	log.Info("Starting prometheus metrics.")
	if err := operatormetrics.StartMetrics(); err != nil {
		log.Error(err, "Failed to start metrics service")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
