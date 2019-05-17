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

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// MetricsEndpoint is the port to export metrics on
	MetricsEndpoint = ":8080"
)

var (
	metricPlaceholder = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "placeholder",
		Help: "Placeholder",
	}, []string{"name"})

	metricsList = []prometheus.Collector{
		metricPlaceholder,
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

// UpdatePlaceholderGauge ...
func UpdatePlaceholderGauge() {

	metricPlaceholder.With(prometheus.Labels{"name": "pagerduty-operator"}).Set(float64(1))
}
