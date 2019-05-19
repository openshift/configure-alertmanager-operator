// Copyright 2019 RedHat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"net/http"

	"github.com/openshift/configure-alertmanager-operator/config"
	alertmanager "github.com/openshift/configure-alertmanager-operator/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
)

const (
	// MetricsEndpoint is the port to export metrics on
	MetricsEndpoint = ":8080"
)

var (
	metricPDSecretExists = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pd_secret_exists",
		Help: "Pager Duty secret exists",
	}, []string{"name"})
	metricDMSSecretExists = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dms_secret_exists",
		Help: "Dead Man's Snitch secret exists",
	}, []string{"name"})
	metricAMSecretExists = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "am_secret_exists",
		Help: "AlertManager Config secret exists",
	}, []string{"name"})
	metricAMSecretContainsPD = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "am_secret_contains_pd",
		Help: "AlertManager Config contains configuration for Pager Duty",
	}, []string{"name"})
	metricAMSecretContainsDMS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "am_secret_contains_dms",
		Help: "AlertManager Config contains configuration for Dead Man's Snitch",
	}, []string{"name"})

	metricsList = []prometheus.Collector{
		metricPDSecretExists,
		metricDMSSecretExists,
		metricAMSecretExists,
		metricAMSecretContainsPD,
		metricAMSecretContainsDMS,
	}
)

// StartMetrics register metrics and exposes them
func StartMetrics() {

	// Register metrics and start serving them on /metrics endpoint
	RegisterMetrics()
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(MetricsEndpoint, nil)
}

// RegisterMetrics for the operator
func RegisterMetrics() error {
	for _, metric := range metricsList {
		err := prometheus.Register(metric)
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateSecretsMetrics updates all metrics related to the existance and contents of Secrets
// used by configure-alertmanager-operator.
func UpdateSecretsMetrics(list *corev1.SecretList, amconfig alertmanager.Config) {

	// Default to false.
	pdSecretExists := false
	dmsSecretExists := false
	amSecretExists := false
	amSecretContainsPD := false
	amSecretContainsDMS := false

	// Update the metric if the secret is found in the SecretList.
	for _, secret := range list.Items {
		switch secret.Name {
		case "pd-secret":
			pdSecretExists = true
		case "dms-secret":
			dmsSecretExists = true
		case "alertmanager-main":
			amSecretExists = true
		}
	}

	// Check for the presence of PD and DMS configs inside the AlertManager config and report metrics.
	if amSecretExists {
		if pdSecretExists {
			for _, receiver := range amconfig.Receivers {
				if receiver.Name == "pagerduty" {
					amSecretContainsPD = true
				}
			}
		}
		if dmsSecretExists {
			for _, receiver := range amconfig.Receivers {
				if receiver.Name == "watchdog" {
					amSecretContainsDMS = true
				}
			}
		}
	}

	// Only set metrics once per run.
	if pdSecretExists {
		metricPDSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(1))
	} else {
		metricPDSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(0))
	}
	if dmsSecretExists {
		metricDMSSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(1))
	} else {
		metricDMSSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(0))
	}
	if amSecretExists {
		metricAMSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(1))
	} else {
		metricAMSecretExists.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(0))
	}
	if amSecretContainsPD {
		metricAMSecretContainsPD.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(1))
	} else {
		metricAMSecretContainsPD.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(0))
	}
	if amSecretContainsDMS {
		metricAMSecretContainsDMS.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(1))
	} else {
		metricAMSecretContainsDMS.With(prometheus.Labels{"name": config.OperatorName}).Set(float64(0))
	}
}
