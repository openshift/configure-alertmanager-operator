package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/configure-alertmanager-operator/pkg/apis"
	"github.com/openshift/configure-alertmanager-operator/pkg/controller"
	operatormetrics "github.com/openshift/configure-alertmanager-operator/pkg/metrics"
	operatorconfig "github.com/openshift/configure-alertmanager-operator/config"

	"github.com/operator-framework/operator-sdk/pkg/leader"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)
var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func main() {
	// Add the zap logger flag set to the CLI. The flag set must
	// be added before calling pflag.Parse().
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.Logger())

	printVersion()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx := context.TODO()

	// Become the leader before proceeding
	err = leader.Become(ctx, "configure-alertmanager-operator-lock")
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := configv1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := monitoringv1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "error registering prometheus monitoring objects")
		os.Exit(1)
	}

	err = operatorconfig.IsFedramp()
	if err != nil {
		log.Error(err, "Failed to get fedramp value")
		osd.Exit(1)
	}
	if operatorconfig.IsFedramp() {
		log.Info("Running in fedramp environment")
	}
	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
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
	if err != nil {
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

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
