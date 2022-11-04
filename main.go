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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/openshift/configure-alertmanager-operator/controllers"
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
		Scheme:                 scheme,
		MetricsBindAddress:     fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
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

	err = leader.Become(context.TODO(), "configure-alertmanager-operator-lock")
	if err != nil {
		setupLog.Error(err, "Failed to retry for leader lock")
		os.Exit(1)
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
		log.Error(err, "error creating metrics Service")
	} else {
		log.Info("Created Service")
		err = mgr.GetClient().Create(context.TODO(), sm)
		if err != nil {
			log.Error(err, "error creating metrics ServiceMonitor")
		} else {
			log.Info("Created ServiceMonitor")
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
