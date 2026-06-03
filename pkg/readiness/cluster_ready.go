package readiness

//go:generate mockgen -destination zz_generated_mocks.go -package readiness -source=cluster_ready.go

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("readiness")

// Impl is a concrete instance of the readiness engine.
type Impl struct {
	// Client is a controller-runtime client capable of querying k8s.
	Client client.Client
	// result is what the calling Reconcile should return if it is otherwise successful.
	result reconcile.Result
	// ready indicates whether the cluster is considered ready. Once this is true,
	// Check() is a no-op.
	ready bool
	// clusterCreationTime caches the birth time of the cluster so we only have to
	// query prometheus once.
	clusterCreationTime time.Time
	// promAPI is a handle to the prometheus API client
	promAPI promv1.API
}

// Interface is the interface for the readiness engine.
type Interface interface {
	IsReady() (bool, error)
	Result() reconcile.Result
	setClusterCreationTime() error
	clusterTooOld(int) bool
	setPromAPI() error
}

var _ Interface = &Impl{}

const (
	maxClusterAgeKey     = "MAX_CLUSTER_AGE_MINUTES"
	maxClusterAgeDefault = 90
)

// IsReady determines whether the cluster is ready for PagerDuty configuration.
// Ready is true if:
//   - a previous check has already succeeded (cached);
//   - all ClusterOperators have Progressing=false; or
//   - the cluster is older than maxClusterAgeMinutes (fallback)
func (impl *Impl) IsReady() (bool, error) {
	if impl.ready {
		log.Info("DEBUG: Using cached positive cluster readiness.")
		return impl.ready, nil
	}

	impl.result = reconcile.Result{}

	// Primary check: all ClusterOperators have stopped progressing
	coList := &configv1.ClusterOperatorList{}
	if err := impl.Client.List(context.TODO(), coList); err != nil {
		log.Error(err, "Failed to list ClusterOperators, falling through to age check")
	} else if len(coList.Items) > 0 {
		allReady := true
		for _, co := range coList.Items {
			for _, cond := range co.Status.Conditions {
				if cond.Type == configv1.OperatorProgressing && cond.Status == configv1.ConditionTrue {
					allReady = false
					break
				}
				if cond.Type == configv1.OperatorDegraded && cond.Status == configv1.ConditionTrue {
					allReady = false
					break
				}
				if cond.Type == configv1.OperatorAvailable && cond.Status == configv1.ConditionFalse {
					allReady = false
					break
				}
			}
			if !allReady {
				break
			}
		}
		if allReady {
			log.Info(fmt.Sprintf("INFO: All %d ClusterOperators are Available, not Progressing, and not Degraded. Cluster is ready.", len(coList.Items)))
			impl.ready = true
			return impl.ready, nil
		}
	}

	// Fallback: cluster age check
	if err := impl.setClusterCreationTime(); err != nil {
		log.Error(err, "Failed to determine cluster creation time")
		impl.result = reconcile.Result{Requeue: true, RequeueAfter: time.Second}
		return false, nil
	}
	maxClusterAge, err := getEnvInt(maxClusterAgeKey, maxClusterAgeDefault)
	if err != nil {
		return false, err
	}
	if impl.clusterTooOld(maxClusterAge) {
		log.Info(fmt.Sprintf("INFO: Cluster is older than %d minutes. Declaring ready.", maxClusterAge))
		impl.ready = true
		return impl.ready, nil
	}

	delay := 30 * time.Second
	log.Info(fmt.Sprintf("INFO: ClusterOperators still progressing. Requeueing after %v.", delay))
	impl.result = reconcile.Result{Requeue: true, RequeueAfter: delay}
	return false, nil
}

func (impl *Impl) Result() reconcile.Result {
	return impl.result
}

func (impl *Impl) setPromAPI() error {
	rawToken, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return fmt.Errorf("couldn't read token file: %w", err)
	}

	client, err := api.NewClient(api.Config{
		Address: "https://prometheus-k8s.openshift-monitoring.svc:9091",
		RoundTripper: &http.Transport{
			Proxy: func(request *http.Request) (*url.URL, error) {
				request.Header.Add("Authorization", "Bearer "+string(rawToken))
				return http.ProxyFromEnvironment(request)
			},
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				// disable "G402 (CWE-295): TLS InsecureSkipVerify set true."
				// #nosec G402
				InsecureSkipVerify: true,
			},
			TLSHandshakeTimeout: 10 * time.Second,
		},
	})
	if err != nil {
		return fmt.Errorf("couldn't configure prometheus client: %w", err)
	}

	impl.promAPI = promv1.NewAPI(client)
	return nil
}

func (impl *Impl) setClusterCreationTime() error {
	if !impl.clusterCreationTime.IsZero() {
		return nil
	}

	if err := impl.setPromAPI(); err != nil {
		return fmt.Errorf("couldn't get prometheus API: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, warnings, err := impl.promAPI.Query(ctx, "cluster_version{type=\"initial\"}", time.Now())
	if err != nil {
		return fmt.Errorf("error querying Prometheus: %w", err)
	}
	if len(warnings) > 0 {
		log.Info(fmt.Sprintf("Warnings: %v\n", warnings))
	}

	resultVec := result.(model.Vector)
	earliest := time.Time{}
	for i := 0; i < resultVec.Len(); i++ {
		thisTime := time.Unix(int64(resultVec[i].Value), 0)
		if earliest.IsZero() || thisTime.Before(earliest) {
			earliest = thisTime
		}
	}
	if earliest.IsZero() {
		return fmt.Errorf("failed to determine cluster birth time from prometheus %s result %v", result.Type().String(), result.String())
	}
	impl.clusterCreationTime = earliest
	log.Info(fmt.Sprintf("INFO: Cluster created %v", earliest.UTC()))
	return nil
}

func (impl *Impl) clusterTooOld(maxAgeMinutes int) bool {
	maxAge := time.Now().Add(time.Duration(-maxAgeMinutes) * time.Minute)
	return impl.clusterCreationTime.Before(maxAge)
}

func getEnvInt(key string, def int) (int, error) {
	strVal := os.Getenv(key)
	if strVal == "" {
		return def, nil
	}
	intVal, err := strconv.Atoi(strVal)
	if err != nil {
		return 0, fmt.Errorf("invalid value for env var: %s=%s (expected int): %v", key, strVal, err)
	}
	return intVal, nil
}
