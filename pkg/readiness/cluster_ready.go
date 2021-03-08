package readiness

//go:generate go-bindata -nocompress -nometadata -pkg readiness -o zz_generated_defs.go defs/
//go:generate mockgen -destination zz_generated_mocks.go -package readiness -source=cluster_ready.go

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/openshift/configure-alertmanager-operator/config"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	// Maximum cluster age, in minutes, after whiche we'll assume we don't need to run health checks.
	maxClusterAgeKey = "MAX_CLUSTER_AGE_MINUTES"
	// By default, ignore clusters older than two hours
	maxClusterAgeDefault = 2 * 60

	jobName = "osd-cluster-ready"
)

// IsReady deals with the osd-cluster-ready Job.
// Sets:
// - impl.Ready:
//   true if:
//   - a previous check has already succeeded (a cluster can't become un-ready once it's ready);
//   - an osd-cluster-ready Job has completed successfully; or
//   - the cluster is older than maxClusterAgeMinutes
//   false otherwise.
// - impl.Result: If the caller's reconcile is otherwise successful, it
//   should return the given Result.
// - impl.clusterCreationTime: If it is necessary to check the age of the cluster, this is set so
//   we only have to query prometheus once.
func (impl *Impl) IsReady() (bool, error) {
	if impl.ready {
		log.Info("DEBUG: Using cached positive cluster readiness.")
		return impl.ready, nil
	}

	// Default Result
	impl.result = reconcile.Result{}

	// Readiness job part 1: Grab it, and short out if it was successful.
	job := &batchv1.Job{}
	found := true
	if err := impl.Client.Get(context.TODO(), types.NamespacedName{Namespace: config.OperatorNamespace, Name: jobName}, job); err != nil {
		if !errors.IsNotFound(err) {
			// If we couldn't query k8s, it is fatal for this iteration of the reconcile
			return false, fmt.Errorf("Failed to retrieve %s Job: %v", jobName, err)
		}
		found = false
	}
	// If the job completed successfully, we're done.
	if found && job.Status.Succeeded > 0 {
		log.Info(fmt.Sprintf("INFO: Found a succeeded %s Job.", jobName))
		impl.ready = true
		return impl.ready, nil
	}

	// Cluster age: short out if the cluster is older than the configured value
	if err := impl.setClusterCreationTime(); err != nil {
		log.Error(err, "Failed to determine cluster creation time")
		// If we failed to query prometheus, the cluster isn't ready.
		// We want the main Reconcile loop to proceed, so don't return an error; but
		// we want to requeue rapidly so we can keep checking for cluster birth.
		impl.result = reconcile.Result{Requeue: true, RequeueAfter: time.Second}
		return false, nil
	}
	maxClusterAge, err := getEnvInt(maxClusterAgeKey, maxClusterAgeDefault)
	if err != nil {
		// This is likely to result in a hot loop :(
		return false, err
	}
	if impl.clusterTooOld(maxClusterAge) {
		log.Info(fmt.Sprintf("INFO: Cluster is older than %d minutes. Ignoring health check.", maxClusterAge))
		impl.ready = true
		return impl.ready, nil
	}

	// Readiness job part 2: existing but not (yet) successul
	if found {
		// If the Job is still running, requeue with a little pause.
		if job.Status.Active > 0 {
			log.Info(fmt.Sprintf("INFO: Found an Active %s Job. Will requeue.", jobName))
			impl.result = reconcile.Result{Requeue: true, RequeueAfter: 10 * time.Second}
			return false, nil
		}

		// If we get here, the Job finished unsuccessfully. Delete it so we can recreate it.
		log.Info(fmt.Sprintf("INFO: Deleting failed %s Job", jobName))
		err = impl.Client.Delete(context.TODO(), job)
		// Let the rest of the reconcile proceed, but requeue so we'll come back and reassess the readiness job next time.
		// We requeue whether there's an error (which might be "already gone") or not.
		impl.result = reconcile.Result{Requeue: true}
		return false, err
	}

	// Readiness job part 3: the Job doesn't exist -- create it.
	loadDefTemplate(job, fmt.Sprintf("%s.Job.yaml", jobName))
	log.Info(fmt.Sprintf("INFO: Creating %s Job", jobName))
	log.Info(fmt.Sprintf("DEBUG: Job def: %v", *job))
	err = impl.Client.Create(context.TODO(), job)
	// Let the rest of the reconcile proceed, but requeue so we'll come back and reassess the readiness job next time.
	// We requeue whether there's an error or not.
	impl.result = reconcile.Result{Requeue: true}
	return false, err
}

func (impl *Impl) Result() reconcile.Result {
	return impl.result
}

func (impl *Impl) setPromAPI() error {
	rawToken, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return fmt.Errorf("Couldn't read token file: %v", err)
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
				InsecureSkipVerify: true,
			},
			TLSHandshakeTimeout: 10 * time.Second,
		},
	})
	if err != nil {
		return fmt.Errorf("Couldn't configure prometheus client: %v", err)
	}

	impl.promAPI = promv1.NewAPI(client)
	return nil
}

func (impl *Impl) setClusterCreationTime() error {
	// Is it cached?
	if !impl.clusterCreationTime.IsZero() {
		return nil
	}

	if err := impl.setPromAPI(); err != nil {
		return fmt.Errorf("Couldn't get prometheus API: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	when := time.Now()
	// For testing, do something like this, subtracting the number of hours
	// since you disabled CVO:
	// when := time.Now().Add(-32*time.Hour)
	result, warnings, err := impl.promAPI.Query(ctx, "cluster_version{type=\"initial\"}", when)
	if err != nil {
		return fmt.Errorf("Error querying Prometheus: %v", err)
	}
	if len(warnings) > 0 {
		log.Info(fmt.Sprintf("Warnings: %v\n", warnings))
	}

	log.Info(fmt.Sprintf("DEBUG: Result of type %s:\n%s\n", result.Type().String(), result.String()))
	resultVec := result.(model.Vector)
	earliest := time.Time{}
	for i := 0; i < resultVec.Len(); i++ {
		thisTime := time.Unix(int64(resultVec[i].Value), 0)
		if earliest.IsZero() || thisTime.Before(earliest) {
			earliest = thisTime
		}
	}
	if earliest.IsZero() {
		return fmt.Errorf("Failed to determine cluster birth time from prometheus %s result %v", result.Type().String(), result.String())
	}
	impl.clusterCreationTime = earliest
	log.Info(fmt.Sprintf("INFO: Cluster created %v", earliest.UTC()))
	return nil
}

func (impl *Impl) clusterTooOld(maxAgeMinutes int) bool {
	maxAge := time.Now().Add(time.Duration(-maxAgeMinutes) * time.Minute)
	return impl.clusterCreationTime.Before(maxAge)
}

// getEnvInt returns the integer value of the environment variable with the specified `key`.
// If the env var is unspecified/empty, the `def` value is returned.
// The error is non-nil if the env var is nonempty but cannot be parsed as an int.
func getEnvInt(key string, def int) (int, error) {
	var intVal int
	var err error

	strVal := os.Getenv(key)

	if strVal == "" {
		// Env var unset; use the default
		return def, nil
	}

	if intVal, err = strconv.Atoi(strVal); err != nil {
		return 0, fmt.Errorf("Invalid value for env var: %s=%s (expected int): %v", key, strVal, err)
	}

	return intVal, nil
}

func loadDefTemplate(receiver runtime.Object, defFile string) {
	if err := yaml.Unmarshal(MustAsset(filepath.Join("defs", defFile)), receiver); err != nil {
		panic(fmt.Sprintf("Couldn't load %s: %s", defFile, err.Error()))
	}
	// TODO: Why aren't these coming in from the def?
	job := receiver.(*batchv1.Job)
	job.TypeMeta = v1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"}
	job.ObjectMeta = v1.ObjectMeta{Name: jobName, Namespace: config.OperatorNamespace}
	job.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullAlways
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
	job.Spec.Template.Spec.ServiceAccountName = jobName
}
