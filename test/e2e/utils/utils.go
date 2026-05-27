package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/onsi/gomega"
	amconfig "github.com/prometheus/alertmanager/config"
	"gopkg.in/yaml.v2"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
)

const (
	AlertmanagerMainSecret = "alertmanager-main"
	AlertmanagerConfigKey  = "alertmanager.yaml"

	// Reconciliation polling defaults
	ReconcileTimeout  = 2 * time.Minute
	ReconcileInterval = 5 * time.Second
)

// ConfigMinimal is a minimal struct for asserting on alertmanager config
// structure without depending on Prometheus internal types.
type ConfigMinimal struct {
	Global       *GlobalMinimal       `yaml:"global,omitempty"`
	Route        *RouteMinimal        `yaml:"route,omitempty"`
	Receivers    []ReceiverMinimal    `yaml:"receivers,omitempty"`
	InhibitRules []InhibitRuleMinimal `yaml:"inhibit_rules,omitempty"`
}

// GlobalMinimal holds global alertmanager config fields used in e2e.
type GlobalMinimal struct {
	PagerdutyURL string `yaml:"pagerduty_url,omitempty"`
}

// RouteMinimal holds a single route node for e2e assertions.
type RouteMinimal struct {
	Receiver string            `yaml:"receiver,omitempty"`
	Match    map[string]string `yaml:"match,omitempty"`
	MatchRE  map[string]string `yaml:"match_re,omitempty"`
	Routes   []*RouteMinimal   `yaml:"routes,omitempty"`
}

// ReceiverMinimal holds receiver name for e2e assertions.
type ReceiverMinimal struct {
	Name string `yaml:"name"`
}

// InhibitRuleMinimal holds inhibit rule fields for e2e assertions.
type InhibitRuleMinimal struct {
	SourceMatch   map[string]string `yaml:"source_match,omitempty"`
	TargetMatchRE map[string]string `yaml:"target_match_re,omitempty"`
	Equal         []string          `yaml:"equal,omitempty"`
}

// GetAlertmanagerConfigBytes returns the raw alertmanager.yaml from the alertmanager-main secret.
func GetAlertmanagerConfigBytes(ctx context.Context, client *resources.Resources, namespace string) ([]byte, error) {
	var secret v1.Secret
	if err := client.Get(ctx, AlertmanagerMainSecret, namespace, &secret); err != nil {
		return nil, err
	}
	data, ok := secret.Data[AlertmanagerConfigKey]
	if !ok || len(data) == 0 {
		return nil, nil
	}
	return data, nil
}

// LoadAndValidateAlertmanagerConfig validates the config using Prometheus Alertmanager's
// official Load and returns the parsed config.
func LoadAndValidateAlertmanagerConfig(data []byte) (*amconfig.Config, error) {
	return amconfig.Load(string(data))
}

// ParseConfigMinimal parses the YAML into a minimal struct for assertions.
func ParseConfigMinimal(data []byte) (*ConfigMinimal, error) {
	var cfg ConfigMinimal
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ReceiverExists returns true if a receiver with the given name exists in the config.
func ReceiverExists(cfg *ConfigMinimal, name string) bool {
	for _, r := range cfg.Receivers {
		if r.Name == name {
			return true
		}
	}
	return false
}

// HasGlobalPagerdutyURL returns true if global.pagerduty_url is set.
func HasGlobalPagerdutyURL(cfg *ConfigMinimal) bool {
	return cfg.Global != nil && cfg.Global.PagerdutyURL != ""
}

// HasInhibitRuleWithSourceMatch returns true if any inhibit rule has the given source_match key-value.
func HasInhibitRuleWithSourceMatch(cfg *ConfigMinimal, key, value string) bool {
	for _, ir := range cfg.InhibitRules {
		if ir.SourceMatch != nil && ir.SourceMatch[key] == value {
			return true
		}
	}
	return false
}

// RouteTreeContainsReceiver returns true if any route in the tree has the given receiver.
func RouteTreeContainsReceiver(route *RouteMinimal, receiverName string) bool {
	if route == nil {
		return false
	}
	if route.Receiver == receiverName {
		return true
	}
	for _, r := range route.Routes {
		if RouteTreeContainsReceiver(r, receiverName) {
			return true
		}
	}
	return false
}

// RouteTreeHasMatch returns true if any route in the tree has a match entry with the given key and value.
func RouteTreeHasMatch(route *RouteMinimal, key, value string) bool {
	if route == nil {
		return false
	}
	if route.Match != nil && route.Match[key] == value {
		return true
	}
	for _, r := range route.Routes {
		if RouteTreeHasMatch(r, key, value) {
			return true
		}
	}
	return false
}

// PagerdutyConfigMinimal holds PagerDuty receiver config fields for e2e assertions.
type PagerdutyConfigMinimal struct {
	RoutingKey string `yaml:"routing_key,omitempty"`
}

// WebhookConfigMinimal holds webhook receiver config fields for e2e assertions.
type WebhookConfigMinimal struct {
	URL string `yaml:"url,omitempty"`
}

// ReceiverMinimalFull extends ReceiverMinimal with receiver config details.
type ReceiverMinimalFull struct {
	Name             string                   `yaml:"name"`
	PagerdutyConfigs []PagerdutyConfigMinimal `yaml:"pagerduty_configs,omitempty"`
	WebhookConfigs   []WebhookConfigMinimal   `yaml:"webhook_configs,omitempty"`
}

// ConfigMinimalFull is an extended config struct that parses receiver details.
type ConfigMinimalFull struct {
	Global       *GlobalMinimal        `yaml:"global,omitempty"`
	Route        *RouteMinimal         `yaml:"route,omitempty"`
	Receivers    []ReceiverMinimalFull `yaml:"receivers,omitempty"`
	InhibitRules []InhibitRuleMinimal  `yaml:"inhibit_rules,omitempty"`
}

// ParseConfigMinimalFull parses the YAML into an extended struct with receiver details.
func ParseConfigMinimalFull(data []byte) (*ConfigMinimalFull, error) {
	var cfg ConfigMinimalFull
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetReceiverByName finds a receiver by name from the full config.
func GetReceiverByName(cfg *ConfigMinimalFull, name string) *ReceiverMinimalFull {
	for i := range cfg.Receivers {
		if cfg.Receivers[i].Name == name {
			return &cfg.Receivers[i]
		}
	}
	return nil
}

// ReceiverExistsFull returns true if a receiver with the given name exists in the full config.
func ReceiverExistsFull(cfg *ConfigMinimalFull, name string) bool {
	return GetReceiverByName(cfg, name) != nil
}

// BackupSecret captures a deep copy of a secret for later restore.
func BackupSecret(ctx context.Context, client *resources.Resources, name, namespace string) (*v1.Secret, error) {
	var secret v1.Secret
	if err := client.Get(ctx, name, namespace, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s for backup: %w", namespace, name, err)
	}
	return secret.DeepCopy(), nil
}

// RestoreSecret restores a secret's Data from a backup copy.
func RestoreSecret(ctx context.Context, client *resources.Resources, original *v1.Secret) error {
	var current v1.Secret
	err := client.Get(ctx, original.Name, original.Namespace, &current)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Secret was deleted; recreate it
			recreated := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      original.Name,
					Namespace: original.Namespace,
					Labels:    original.Labels,
				},
				Data: original.Data,
				Type: original.Type,
			}
			return client.Create(ctx, recreated)
		}
		return fmt.Errorf("failed to get secret %s/%s for restore: %w", original.Namespace, original.Name, err)
	}
	current.Data = original.Data
	return client.Update(ctx, &current)
}

// BackupConfigMap captures a deep copy of a ConfigMap for later restore.
func BackupConfigMap(ctx context.Context, client *resources.Resources, name, namespace string) (*v1.ConfigMap, error) {
	var cm v1.ConfigMap
	if err := client.Get(ctx, name, namespace, &cm); err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s for backup: %w", namespace, name, err)
	}
	return cm.DeepCopy(), nil
}

// RestoreConfigMap restores a ConfigMap's Data from a backup copy.
func RestoreConfigMap(ctx context.Context, client *resources.Resources, original *v1.ConfigMap) error {
	var current v1.ConfigMap
	if err := client.Get(ctx, original.Name, original.Namespace, &current); err != nil {
		return fmt.Errorf("failed to get configmap %s/%s for restore: %w", original.Namespace, original.Name, err)
	}
	current.Data = original.Data
	if original.Annotations != nil {
		current.Annotations = original.Annotations
	}
	return client.Update(ctx, &current)
}

// UpdateSecretKey updates a single key in a secret's Data.
func UpdateSecretKey(ctx context.Context, client *resources.Resources, secretName, namespace, key string, value []byte) error {
	var secret v1.Secret
	if err := client.Get(ctx, secretName, namespace, &secret); err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[key] = value
	return client.Update(ctx, &secret)
}

// UpdateConfigMapKey updates a single key in a ConfigMap's Data.
func UpdateConfigMapKey(ctx context.Context, client *resources.Resources, cmName, namespace, key, value string) error {
	var cm v1.ConfigMap
	if err := client.Get(ctx, cmName, namespace, &cm); err != nil {
		return fmt.Errorf("failed to get configmap %s/%s: %w", namespace, cmName, err)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[key] = value
	return client.Update(ctx, &cm)
}

// AnnotateConfigMap adds or updates an annotation on a ConfigMap.
func AnnotateConfigMap(ctx context.Context, client *resources.Resources, cmName, namespace, annotationKey, annotationValue string) error {
	var cm v1.ConfigMap
	if err := client.Get(ctx, cmName, namespace, &cm); err != nil {
		return fmt.Errorf("failed to get configmap %s/%s: %w", namespace, cmName, err)
	}
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	cm.Annotations[annotationKey] = annotationValue
	return client.Update(ctx, &cm)
}

// CreateSecret creates a new secret with the given data.
func CreateSecret(ctx context.Context, client *resources.Resources, name, namespace string, data map[string][]byte) error {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
	return client.Create(ctx, secret)
}

// DeleteSecret deletes a secret by name and namespace.
func DeleteSecret(ctx context.Context, client *resources.Resources, name, namespace string) error {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return client.Delete(ctx, secret)
}

// SecretExists checks if a secret exists in a namespace.
func SecretExists(ctx context.Context, client *resources.Resources, name, namespace string) bool {
	var secret v1.Secret
	err := client.Get(ctx, name, namespace, &secret)
	return err == nil
}

// ConfigMapExists checks if a ConfigMap exists in a namespace.
func ConfigMapExists(ctx context.Context, client *resources.Resources, name, namespace string) bool {
	var cm v1.ConfigMap
	err := client.Get(ctx, name, namespace, &cm)
	return err == nil
}

// --- Reconciliation polling helpers ---

// WaitForReceiverPresent polls alertmanager-main until a receiver with the given name appears.
// Returns an error function suitable for use with Eventually.
func WaitForReceiverPresent(ctx context.Context, client *resources.Resources, namespace, receiverName string) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
		configBytes, err := GetAlertmanagerConfigBytes(ctx, client, namespace)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get alertmanager config")
		g.Expect(configBytes).ShouldNot(gomega.BeEmpty(), "alertmanager config is empty")

		cfg, err := ParseConfigMinimalFull(configBytes)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to parse alertmanager config")
		g.Expect(ReceiverExistsFull(cfg, receiverName)).To(gomega.BeTrue(),
			fmt.Sprintf("receiver %q should be present in alertmanager config", receiverName))
	}
}

// WaitForReceiverAbsent polls alertmanager-main until a receiver with the given name disappears.
func WaitForReceiverAbsent(ctx context.Context, client *resources.Resources, namespace, receiverName string) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
		configBytes, err := GetAlertmanagerConfigBytes(ctx, client, namespace)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get alertmanager config")
		g.Expect(configBytes).ShouldNot(gomega.BeEmpty(), "alertmanager config is empty")

		cfg, err := ParseConfigMinimalFull(configBytes)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to parse alertmanager config")
		g.Expect(ReceiverExistsFull(cfg, receiverName)).To(gomega.BeFalse(),
			fmt.Sprintf("receiver %q should be absent from alertmanager config", receiverName))
	}
}

// WaitForPagerdutyRoutingKey polls alertmanager-main until the pagerduty receiver has the expected routing key.
func WaitForPagerdutyRoutingKey(ctx context.Context, client *resources.Resources, namespace, expectedKey string) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
		configBytes, err := GetAlertmanagerConfigBytes(ctx, client, namespace)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get alertmanager config")

		cfg, err := ParseConfigMinimalFull(configBytes)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to parse alertmanager config")

		receiver := GetReceiverByName(cfg, "pagerduty")
		g.Expect(receiver).ShouldNot(gomega.BeNil(), "pagerduty receiver should exist")
		g.Expect(receiver.PagerdutyConfigs).ShouldNot(gomega.BeEmpty(), "pagerduty receiver should have configs")
		g.Expect(receiver.PagerdutyConfigs[0].RoutingKey).To(gomega.Equal(expectedKey),
			fmt.Sprintf("pagerduty routing_key should be %q", expectedKey))
	}
}

// WaitForWebhookURL polls alertmanager-main until the named receiver has the expected webhook URL.
func WaitForWebhookURL(ctx context.Context, client *resources.Resources, namespace, receiverName, expectedURL string) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
		configBytes, err := GetAlertmanagerConfigBytes(ctx, client, namespace)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get alertmanager config")

		cfg, err := ParseConfigMinimalFull(configBytes)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to parse alertmanager config")

		receiver := GetReceiverByName(cfg, receiverName)
		g.Expect(receiver).ShouldNot(gomega.BeNil(), fmt.Sprintf("receiver %q should exist", receiverName))
		g.Expect(receiver.WebhookConfigs).ShouldNot(gomega.BeEmpty(),
			fmt.Sprintf("receiver %q should have webhook configs", receiverName))
		g.Expect(receiver.WebhookConfigs[0].URL).To(gomega.Equal(expectedURL),
			fmt.Sprintf("receiver %q webhook URL should be %q", receiverName, expectedURL))
	}
}

// GetAlertmanagerSecretResourceVersion returns the resourceVersion of alertmanager-main secret.
func GetAlertmanagerSecretResourceVersion(ctx context.Context, client *resources.Resources, namespace string) (string, error) {
	var secret v1.Secret
	if err := client.Get(ctx, AlertmanagerMainSecret, namespace, &secret); err != nil {
		return "", err
	}
	return secret.ResourceVersion, nil
}

// WaitForResourceVersionChange polls alertmanager-main until its resourceVersion differs from the given one.
func WaitForResourceVersionChange(ctx context.Context, client *resources.Resources, namespace, previousVersion string) func(g gomega.Gomega) {
	return func(g gomega.Gomega) {
		rv, err := GetAlertmanagerSecretResourceVersion(ctx, client, namespace)
		g.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to get alertmanager-main resourceVersion")
		g.Expect(rv).ShouldNot(gomega.Equal(previousVersion),
			"alertmanager-main resourceVersion should have changed after reconciliation")
	}
}

// --- Alertmanager v2 API client ---

// AlertmanagerV2AlertStatus holds the status fields from the Alertmanager v2 API.
type AlertmanagerV2AlertStatus struct {
	State       string   `json:"state"`
	InhibitedBy []string `json:"inhibitedBy"`
}

// AlertmanagerV2Alert holds the fields from the Alertmanager v2 API needed for e2e testing.
type AlertmanagerV2Alert struct {
	Labels map[string]string         `json:"labels"`
	Status AlertmanagerV2AlertStatus `json:"status"`
}

// AlertmanagerClient provides access to the Alertmanager v2 API on an OpenShift cluster.
// GET requests use the external route; POST requests exec into each alertmanager pod
// directly to avoid load-balancing issues (alerts posted via the API are not gossiped
// between peers).
type AlertmanagerClient struct {
	baseURL    string
	httpClient *http.Client
	kubeClient kubernetes.Interface
	restConfig *rest.Config
}

// NewAlertmanagerClient creates an AlertmanagerClient by discovering the alertmanager-main
// route and setting up bearer token authentication with the cluster's router CA.
func NewAlertmanagerClient(ctx context.Context, dynClient dynamic.Interface, kubeClient kubernetes.Interface, restConfig *rest.Config) (*AlertmanagerClient, error) {
	routeGVR := schema.GroupVersionResource{
		Group:    "route.openshift.io",
		Version:  "v1",
		Resource: "routes",
	}
	route, err := dynClient.Resource(routeGVR).Namespace("openshift-monitoring").Get(ctx, "alertmanager-main", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get alertmanager-main route: %w", err)
	}
	ingress, found, err := unstructured.NestedSlice(route.Object, "status", "ingress")
	if err != nil || !found || len(ingress) == 0 {
		return nil, fmt.Errorf("alertmanager-main route has no ingress status")
	}
	var host string
	for _, entry := range ingress {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		conditions, _, _ := unstructured.NestedSlice(entryMap, "conditions")
		admitted := false
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _, _ := unstructured.NestedString(condMap, "type")
			condStatus, _, _ := unstructured.NestedString(condMap, "status")
			if condType == "Admitted" && condStatus == "True" {
				admitted = true
				break
			}
		}
		if !admitted {
			continue
		}
		h, found, _ := unstructured.NestedString(entryMap, "host")
		if found && h != "" {
			host = h
			break
		}
	}
	if host == "" {
		return nil, fmt.Errorf("alertmanager-main route has no admitted ingress with a host")
	}

	expirationSeconds := int64(24 * time.Hour / time.Second)
	tokenReq, err := kubeClient.CoreV1().ServiceAccounts("openshift-monitoring").CreateToken(ctx, "prometheus-k8s",
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expirationSeconds},
		}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create token for prometheus-k8s SA: %w", err)
	}

	routerCAConfigMap, err := kubeClient.CoreV1().ConfigMaps("openshift-config-managed").Get(ctx, "default-ingress-cert", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get router CA configmap: %w", err)
	}
	bundlePEM := []byte(routerCAConfigMap.Data["ca-bundle.crt"])
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundlePEM) {
		return nil, fmt.Errorf("failed to parse any certificates from router CA bundle")
	}

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: transport.NewBearerAuthRoundTripper(
			tokenReq.Status.Token,
			&http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
				TLSClientConfig: &tls.Config{
					RootCAs:    roots,
					ServerName: host,
					MinVersion: tls.VersionTLS12,
				},
			},
		),
	}

	return &AlertmanagerClient{
		baseURL:    "https://" + host,
		httpClient: httpClient,
		kubeClient: kubeClient,
		restConfig: restConfig,
	}, nil
}

// GetAlerts queries the Alertmanager v2 API for alerts matching the given filters.
// Filters use the Alertmanager filter syntax, e.g. `alertname="ClusterOperatorDown"`.
func (c *AlertmanagerClient) GetAlerts(ctx context.Context, filters ...string) ([]AlertmanagerV2Alert, error) {
	u, err := url.Parse(c.baseURL + "/api/v2/alerts")
	if err != nil {
		return nil, fmt.Errorf("failed to parse alertmanager URL: %w", err)
	}
	q := u.Query()
	for _, f := range filters {
		q.Add("filter", f)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alertmanager request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("alertmanager returned status %d: %s", resp.StatusCode, string(preview))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read alertmanager response: %w", err)
	}

	var alerts []AlertmanagerV2Alert
	if err := json.Unmarshal(body, &alerts); err != nil {
		return nil, fmt.Errorf("failed to parse alertmanager response: %w", err)
	}
	return alerts, nil
}

// GetAlertsByName returns alerts from the Alertmanager v2 API matching the given
// alertname and optional name label.
func (c *AlertmanagerClient) GetAlertsByName(ctx context.Context, alertname, name string) ([]AlertmanagerV2Alert, error) {
	filters := []string{fmt.Sprintf(`alertname="%s"`, alertname)}
	if name != "" {
		filters = append(filters, fmt.Sprintf(`name="%s"`, name))
	}
	return c.GetAlerts(ctx, filters...)
}

// AlertmanagerV2PostAlert is the payload for posting synthetic alerts to the Alertmanager v2 API.
type AlertmanagerV2PostAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
	EndsAt      string            `json:"endsAt,omitempty"`
}

// PostAlerts sends synthetic alerts to every alertmanager pod via exec.
// Alerts posted to the Alertmanager API are not gossiped between peers,
// so we must post to each pod individually to ensure the alert is visible
// regardless of which pod handles subsequent GET requests.
func (c *AlertmanagerClient) PostAlerts(ctx context.Context, alerts []AlertmanagerV2PostAlert) error {
	body, err := json.Marshal(alerts)
	if err != nil {
		return fmt.Errorf("failed to marshal alerts: %w", err)
	}

	pods, err := c.kubeClient.CoreV1().Pods("openshift-monitoring").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=alertmanager",
	})
	if err != nil {
		return fmt.Errorf("failed to list alertmanager pods: %w", err)
	}

	for _, pod := range pods.Items {
		if err := c.execCurlPost(ctx, pod.Name, body); err != nil {
			return fmt.Errorf("failed to post alerts to %s: %w", pod.Name, err)
		}
	}
	return nil
}

func (c *AlertmanagerClient) execCurlPost(ctx context.Context, podName string, jsonBody []byte) error {
	req := c.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace("openshift-monitoring").
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: "alertmanager",
			Command: []string{
				"curl", "-sf",
				"-X", "POST",
				"-H", "Content-Type: application/json",
				"-d", string(jsonBody),
				"http://localhost:9093/api/v2/alerts",
			},
			Stdout: true,
			Stderr: true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return fmt.Errorf("exec failed (stderr: %s): %w", stderr.String(), err)
	}
	return nil
}

// ResolveAlerts resolves previously posted synthetic alerts by setting endsAt to the past.
func (c *AlertmanagerClient) ResolveAlerts(ctx context.Context, alerts []AlertmanagerV2PostAlert) error {
	resolved := make([]AlertmanagerV2PostAlert, len(alerts))
	past := time.Now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	for i, a := range alerts {
		resolved[i] = AlertmanagerV2PostAlert{
			Labels:      a.Labels,
			Annotations: a.Annotations,
			EndsAt:      past,
		}
	}
	return c.PostAlerts(ctx, resolved)
}
