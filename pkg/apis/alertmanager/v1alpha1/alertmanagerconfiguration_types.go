package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	alertmanagerconfig "github.com/openshift/configure-alertmanager-operator/pkg/types"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlertManagerConfigurationList contains a list of AlertManagerConfiguration
type AlertManagerConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertManagerConfiguration `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlertManagerConfiguration is the Schema for the alertmanagerconfigurations API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=alertmanagerconfigurations,scope=Namespaced
type AlertManagerConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertManagerConfigurationSpec   `json:"spec,omitempty"`
	Status AlertManagerConfigurationStatus `json:"status,omitempty"`
}

// AlertManagerConfigurationSpec defines the desired state of AlertManagerConfiguration
type AlertManagerConfigurationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	// The Alertmanager route definition for alerts matching the
	// resource’s namespace. It will be added to the generated
	// Alertmanager configuration as a first-level route.
	Route Route `json:"route,omitempty"`

	// List of receivers.
	Receivers []Receiver `json:"receivers,omitempty"`

	// TODO: enable this
	// List of inhibition rules. The rules will only apply to
	// alerts matching the resource’s namespace.
	// InhibitRules []InhibitRule `json:"inhibitRules,omitempty"`
}

// AlertManagerConfigurationStatus defines the observed state of AlertManagerConfiguration
type AlertManagerConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// Route defines a node in a routing tree and its children. ts
// optional configuration parameters are inherited from its parent
// node if not set.
type Route struct {
	// Name of the receiver for this route. If present, it should
	// be listed in the “receivers” field. The field can be
	// omitted only for nested routes otherwise it is mandatory.
	Receiver string `json:"receiver,omitempty"`

	// List of labels to group by.
	GroupBy []string `json:"groupBy,omitempty"`

	// How long to wait before sending the
	// initial notification. Must match the regular expression
	// [0-9]+(ms|s|m|h) (milliseconds seconds minutes hours).
	// +kubebuilder:validation:Pattern=`[0-9]+[ms,s,m,h]{1}`
	GroupWait string `json:"groupWait,omitempty"`

	// How long to wait before sending an
	// updated notification. Must match the regular expression
	// [0-9]+(ms|s|m|h) (milliseconds seconds minutes hours).
	// +kubebuilder:validation:Pattern=`[0-9]+[ms,s,m,h]{1}`
	GroupInterval string `json:"groupInterval,omitempty"`

	// How long to wait before repeating the last
	// notification. Must match the regular expression
	// [0-9]+(ms|s|m|h) (milliseconds seconds minutes hours).
	// +kubebuilder:validation:Pattern=`[0-9]+[ms,s,m,h]{1}`
	RepeatInterval string `json:"repeatInterval,omitempty"`

	// List of matchers that the alert’s labels should match. The
	// first-level route will always include a matcher on the
	// resource’s namespace.
	Matchers []Matcher `json:"matchers,omitempty"`

	// Boolean indicating whether an alert should continue
	// matching subsequent sibling nodes. It will always be
	// overridden to true for the first-level route by the
	// Prometheus operator.
	Continue bool `json:"continue,omitempty"`

	// Definition of nested/child routes. Alertmanager routes are
	// recursive structures: one route can contain an arbitrary
	// level of nested routes.
	Routes []Route `json:"routes,omitempty"`
}

// ToAMRoute converts a Route specified in an AlertManagerConfiguration
// CR, to the Route type expected for serialization into the Alertmanager
// YAML config file.
func (r Route) ToAMRoute(amcObjectMeta metav1.ObjectMeta) *alertmanagerconfig.Route {
	return r.toAMRoute(amcObjectMeta, true)
}

func (r Route) toAMRoute(amcObjectMeta metav1.ObjectMeta, firstLevelRoute bool) *alertmanagerconfig.Route {

	cont := r.Continue

	// "continue" is enforced to true for top level route in an
	// AlertManagerConfiguration CR, and namespace match/matchre are
	// replaced with a namespace matcher for the namespace the CR is in
	if firstLevelRoute {
		cont = true

		// TODO: operator currently only watches own namespace, so need
		// to figure out if CRs should only be in there or in their own
		// namespaces
		r.Matchers = append(r.Matchers, Matcher{Name: "namespace", Value: amcObjectMeta.Namespace})
		for _, matcher := range r.Matchers {
			if matcher.Name == "namespace" {
				matcher.Value = amcObjectMeta.Namespace
				matcher.Regex = false
				// Don't break here, as there could be more than
				// one "namespace" matcher, and it's easier to
				// just force-set them all than to delete
				// elements from a slice
			}
		}
	}

	match := map[string]string{}
	matchRE := map[string]string{}
	for _, matcher := range r.Matchers {
		if matcher.Regex {
			matchRE[matcher.Name] = matcher.Value
		} else {
			match[matcher.Name] = matcher.Value
		}
	}

	routes := []*alertmanagerconfig.Route{}
	for _, route := range r.Routes {
		amRoute := route.toAMRoute(amcObjectMeta, false)
		routes = append(routes, amRoute)
	}

	return &alertmanagerconfig.Route{
		Receiver:       prefixNamespaceName(amcObjectMeta.Namespace, amcObjectMeta.Name, r.Receiver),
		GroupByStr:     r.GroupBy,
		GroupWait:      r.GroupWait,
		GroupInterval:  r.GroupInterval,
		RepeatInterval: r.RepeatInterval,
		Continue:       cont,
		Match:          match,
		MatchRE:        matchRE,
	}
}

// Receiver is a named configuration of one or more notification
// integrations. Currently supported integrations limited to Email, PagerDuty
// and Webhooks.
type Receiver struct {
	// Name of the receiver. Must be unique across all items from the list.
	Name string `json:"name"`

	// TODO: enable this
	// List of email receivers
	// Emails []EmailReceiver `json:"emails,omitempty"`

	// List of OpsGenie receivers
	PagerDutys []PagerDutyReceiver `json:"pagerdutys,omitempty"`

	// List of Webhook receivers
	Webhooks []WebhookReceiver `json:"webhooks,omitempty"`
}

// ToAMReceiver converts a Receiver specified in an AlertManagerConfiguration
// CR, to the Receiver type expected for serialization into the Alertmanager
// YAML config file.
func (r Receiver) ToAMReceiver(
	amcObjectMeta metav1.ObjectMeta,
	getValueFromSecretKeySelector func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error),
) *alertmanagerconfig.Receiver {

	pagerdutyConfigs := []*alertmanagerconfig.PagerdutyConfig{}
	for _, p := range r.PagerDutys {
		pagerdutyConfig := p.toAMPagerdutyConfig(amcObjectMeta, getValueFromSecretKeySelector)
		pagerdutyConfigs = append(pagerdutyConfigs, &pagerdutyConfig)
	}

	webhookConfigs := []*alertmanagerconfig.WebhookConfig{}
	for _, w := range r.Webhooks {
		webhookConfig := w.toAMWebhookConfig(amcObjectMeta, getValueFromSecretKeySelector)
		webhookConfigs = append(webhookConfigs, &webhookConfig)
	}

	return &alertmanagerconfig.Receiver{
		Name:             prefixNamespaceName(amcObjectMeta.Namespace, amcObjectMeta.Name, r.Name),
		PagerdutyConfigs: pagerdutyConfigs,
		WebhookConfigs:   webhookConfigs,
	}
}

// InhibitRule is an AlertManager configuration option to decide if an
// alert should be muted.
type InhibitRule struct {
	// Matchers that have to be fulfilled in the alerts to be
	// muted. The operator enforces that the alert matches the
	// resource’s namespace.
	TargetMatch []Matcher `json:"targetMatch,omitempty"`

	// Matchers for which one or more alerts have to exist for the
	// inhibition to take effect. The operator enforces that the
	// alert matches the resource’s namespace.
	SourceMatch []Matcher `json:"sourceMatch,omitempty"`

	// Labels that must have an equal value in the source and
	// target alert for the inhibition to take effect.
	Equal []string `json:"equal,omitempty"`
}

// Matcher is a encoding of an alert matcher to be used as a source or
// target in an inhibition rule or route matchers list.
type Matcher struct {
	// Name of the alert's label to match.
	Name string `json:"name"`

	// Value of the alert's label to match.
	Value string `json:"value"`

	// Boolean indicating whether it is a regex-matcher or not.
	Regex bool `json:"regex,omitempty"`
}

// EmailReceiver holds the configuration for an email receiver
type EmailReceiver struct {
	// Whether to send resolved notifications or not.
	SendResolved bool `json:"sendResolved,omitempty"`

	// Email address(es) to send notifications to.
	To string `json:"to"`

	// The sender address.
	From string `json:"from,omitempty"`

	// The address of the SMTP server (in the form of “host:port”).
	Smarthost string `json:"smarthost,omitempty"`

	// The hostname to use when identifying to the SMTP server.
	Hello string `json:"hello,omitempty"`

	// The username for CRAM-MD5, LOGIN and PLAIN authentications.
	AuthUsername string `json:"authUsername,omitempty"`

	// The identity for CRAM-MD5 authentication.
	AuthIdentify string `json:"authIdentify,omitempty"`

	// The secret reference in the AlertmanagerConfiguration
	// namespace that contains the SMTP password for LOGIN and
	// PLAIN authentications.
	AuthPassword *corev1.SecretKeySelector `json:"authPassword,omitempty"`

	// The secret reference in the AlertmanagerConfiguration
	// namespace that contains the SMTP secret for CRAM-MD5
	// authentication.
	AuthSecret *corev1.SecretKeySelector `json:"authSecret,omitempty"`

	// TLS configuration.
	TLSConfig *monitoringv1.TLSConfig `json:"tlsConfig,omitempty"`

	// Requires the use of STARTTLS.
	RequireTLS bool `json:"requireTLS,omitempty"`

	// The HTML body of the email.
	HTML string `json:"html,omitempty"`

	// The text body of the email.
	Text string `json:"text,omitempty"`

	// Additional email headers as list of key/value pairs.
	Headers []KeyValue `json:"headers,omitempty"`
}

// PagerDutyReceiver holds the configuration for a PagerDuty receiver
type PagerDutyReceiver struct {
	// Whether to send resolved notifications or not.
	SendResolved bool `json:"sendResolved,omitempty"`

	// PagerDuty integration key (when using Events API
	// v2). Either this key or service_key needs to be defined.
	RoutingKey *corev1.SecretKeySelector `json:"routingKey,omitempty"`

	// TODO: enable this
	// PagerDuty integration key (when using integration type
	// “Prometheus”). Either this key or routing_key needs to be
	// defined.
	// ServiceKey *corev1.SecretKeySelector `json:"serviceKey,omitempty"`

	// The URL to send requests to.
	URL string `json:"url,omitempty"`

	// Client identification
	Client string `json:"client,omitempty"`

	// Backlink to the sendor of notification.
	ClientURL string `json:"clientUrl,omitempty"`

	// Description of the incident.
	Description string `json:"description,omitempty"`

	// Severity of the incident.
	Severity string `json:"severity,omitempty"`

	// Arbitrary key/value pairs that provide further detail about
	// the incident.
	Details []KeyValue `json:"details,omitempty"`

	// TODO: enable this
	// HTTP client configuration.
	// HTTPConfig *HTTPConfig `json:"httpConfig,omitempty"`
}

// toAMPagerdutyConfig converts a PagerDutyReceiver specified in a Receiver in
// an AlertManagerConfiguration CR, to the type expected for serialization into
// the Alertmanager YAML config file.
func (p PagerDutyReceiver) toAMPagerdutyConfig(
	amcObjectMeta metav1.ObjectMeta,
	getValueFromSecretKeySelector func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error),
) alertmanagerconfig.PagerdutyConfig {

	routingKey := ""
	if p.RoutingKey != nil {
		secretRoutingKey, err := getValueFromSecretKeySelector(amcObjectMeta.Namespace, p.RoutingKey)
		if err == nil {
			routingKey = secretRoutingKey
		}
	}

	details := map[string]string{}
	for _, detail := range p.Details {
		details[detail.Key] = detail.Value
	}

	return alertmanagerconfig.PagerdutyConfig{
		NotifierConfig: alertmanagerconfig.NotifierConfig{VSendResolved: p.SendResolved},
		RoutingKey:     routingKey,
		URL:            p.URL,
		Client:         p.Client,
		ClientURL:      p.ClientURL,
		Description:    p.Description,
		Details:        details,
		Severity:       p.Severity,

		// TODO: are these properties used?
		Class:     "",
		Component: "",
		Group:     "",
	}
}

// WebhookReceiver holds the configuration for a Webhook receiver
type WebhookReceiver struct {
	// Whether to send resolved notifications or not.
	SendResolved bool `json:"sendResolved,omitempty"`

	// The URL to send HTTP POST requests to. 'urlSecret' takes
	// precedence over 'url'. One of 'urlSecret' and 'url' should
	// be defined.
	URL string `json:"url,omitempty"`

	// The URL to send HTTP POST requests to. 'urlSecret' takes
	// precedence over 'url'. One of 'urlSecret' and 'url' should
	// be defined.
	URLSecret *corev1.SecretKeySelector `json:"urlSecret,omitempty"`

	// TODO: enable this
	// HTTP client configuration.
	// HTTPConfig *HTTPConfig `json:"httpConfig,omitempty"`
}

// toAMWebhookConfig converts a WebhookReceiver specified in a Receiver in an
// AlertManagerConfiguration CR, to the type expected for serialization into the
// Alertmanager YAML config file.
func (w WebhookReceiver) toAMWebhookConfig(
	amcObjectMeta metav1.ObjectMeta,
	getValueFromSecretKeySelector func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error),
) alertmanagerconfig.WebhookConfig {
	url := w.URL
	if w.URLSecret != nil {
		secretUrl, err := getValueFromSecretKeySelector(amcObjectMeta.Namespace, w.URLSecret)
		if err == nil && secretUrl != "" {
			url = secretUrl
		}
	}

	return alertmanagerconfig.WebhookConfig{
		NotifierConfig: alertmanagerconfig.NotifierConfig{VSendResolved: w.SendResolved},
		URL:            url,
	}
}

// KeyValue is a generic type to hold a key and value string pair
type KeyValue struct {
	// The key for this keyvalue pair
	Key string `json:"key"`

	// The value for this keyvalue pair
	Value string `json:"value"`
}

// HTTPConfig holds HTTP Configuration details
type HTTPConfig struct {
	// BasicAuth allow an endpoint to authenticate over basic authentication.
	BasicAuth *monitoringv1.BasicAuth `json:"basicAuth,omitempty"`

	// Bearer token for accessing the endpoint.
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`

	// File to read bearer token for accessing the endpoint.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`

	// TLS configuration for accessing the endpoint.
	TLSConfig *monitoringv1.TLSConfig `json:"tlsConfig,omitempty"`

	// Proxy to use for accessing the endpoint.
	ProxyURL string `json:"proxyUrl,omitempty"`
}

func prefixNamespaceName(namespace, name, suffix string) string {
	return namespace + "-" + name + "-" + suffix
}

func init() {
	SchemeBuilder.Register(&AlertManagerConfiguration{}, &AlertManagerConfigurationList{})
}
