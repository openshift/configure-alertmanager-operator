package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	alertmanagerconfig "github.com/openshift/configure-alertmanager-operator/pkg/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are just used to be able to create *bool in the tests where
// needed, since it's not possible to say e.g. `&true` directly. This
// block also needs to be `var` rather than `const` because consts
// would be inlined which would bring it back to `&true`, and fail.
var (
	_true  bool = true
	_false bool = false
)

func TestRoute(t *testing.T) {
	scenarios := []struct {
		Name   string
		Given  Route
		Meta   metav1.ObjectMeta
		Expect *alertmanagerconfig.Route
	}{
		{
			Name:  "should work with empty input",
			Given: Route{},
			Meta:  metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Route{
				Continue: true,
			},
		},
		{
			Name:  "should enforce Continue to be true for first level Route",
			Given: Route{Continue: false},
			Meta:  metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Route{
				Continue: true,
			},
		},
		{
			Name: "should not force Continue for lower level Route",
			Given: Route{
				Routes: []Route{
					{Continue: false},
					{Continue: true},
				},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Route{
				Continue: true,
				Routes: []*alertmanagerconfig.Route{
					{},
					{Continue: true},
				},
			},
		},
		{
			Name: "should split matchers based on regex flag",
			Given: Route{
				Matchers: []Matcher{
					{Name: "test1", Value: "test1"},
					{Name: "test2", Value: "test2", Regex: true},
				},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Route{
				Continue: true,
				Match:    map[string]string{"test1": "test1"},
				MatchRE:  map[string]string{"test2": "test2"},
			},
		},
		{
			Name: "should translate all flags correctly",
			Given: Route{
				Continue:       true,
				Receiver:       "test",
				GroupBy:        []string{"test1", "test2"},
				GroupWait:      "test3",
				GroupInterval:  "test4",
				RepeatInterval: "test5",
				Routes: []Route{
					{Continue: false},
					{Continue: true},
				},
				Matchers: []Matcher{
					{Name: "test1", Value: "test1"},
					{Name: "test2", Value: "test2", Regex: true},
				},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Route{
				Continue:       true,
				Receiver:       "testns-testn-test",
				GroupByStr:     []string{"test1", "test2"},
				GroupWait:      "test3",
				GroupInterval:  "test4",
				RepeatInterval: "test5",
				Routes: []*alertmanagerconfig.Route{
					{},
					{Continue: true},
				},
				Match:   map[string]string{"test1": "test1"},
				MatchRE: map[string]string{"test2": "test2"},
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {
			actual := s.Given.ToAMRoute(s.Meta)

			if !cmp.Equal(actual, s.Expect) {
				t.Fatalf("Actual and expected Routes differ: %v", cmp.Diff(actual, s.Expect))
			}
		})
	}
}

func TestReceiver(t *testing.T) {
	scenarios := []struct {
		Name       string
		Given      Receiver
		Meta       metav1.ObjectMeta
		SecretFunc func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error)
		Expect     *alertmanagerconfig.Receiver
	}{
		{
			Name:   "should prefix name correctly",
			Given:  Receiver{Name: "test"},
			Meta:   metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Receiver{Name: "testns-testn-test"},
		},
		{
			Name: "should handle empty receivers",
			Given: Receiver{
				Name:       "test",
				Emails:     []EmailReceiver{{}, {}},
				PagerDutys: []PagerDutyReceiver{{}, {}},
				Webhooks:   []WebhookReceiver{{}, {}},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Receiver{
				Name:             "testns-testn-test",
				EmailConfigs:     []*alertmanagerconfig.EmailConfig{{}, {}},
				PagerdutyConfigs: []*alertmanagerconfig.PagerdutyConfig{{}, {}},
				WebhookConfigs:   []*alertmanagerconfig.WebhookConfig{{}, {}},
			},
		},
		{
			Name: "should get value from SecretKeySelector",
			Given: Receiver{
				Name: "test",
				Emails: []EmailReceiver{{
					AuthPassword: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "psecret"},
						Key:                  "pkey",
					},
					AuthSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "ssecret"},
						Key:                  "skey",
					},
				}},
				PagerDutys: []PagerDutyReceiver{{
					RoutingKey: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "rksecret"},
						Key:                  "rkey",
					},
				}},
				Webhooks: []WebhookReceiver{{
					URLSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "urlsecret"},
						Key:                  "urlkey",
					},
				}},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Receiver{
				Name: "testns-testn-test",
				EmailConfigs: []*alertmanagerconfig.EmailConfig{{
					AuthPassword: "defaultvalue",
					AuthSecret:   "defaultvalue",
				}},
				PagerdutyConfigs: []*alertmanagerconfig.PagerdutyConfig{{
					RoutingKey: "defaultvalue",
				}},
				WebhookConfigs: []*alertmanagerconfig.WebhookConfig{{
					URL: "defaultvalue",
				}},
			},
		},
		{
			Name: "should prefer URLSecret field over URL field in WebhookReceiver",
			Given: Receiver{
				Name: "test",
				Webhooks: []WebhookReceiver{{
					URL: "not-this",
					URLSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "urlsecret"},
						Key:                  "urlkey",
					},
				}},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Receiver{
				Name: "testns-testn-test",
				WebhookConfigs: []*alertmanagerconfig.WebhookConfig{{
					URL: "defaultvalue",
				}},
			},
		},
		{
			Name: "should translate all fields correctly",
			Given: Receiver{
				Name: "test",
				PagerDutys: []PagerDutyReceiver{{
					SendResolved: true,
					Client:       "test-client",
					ClientURL:    "https://example.com/v1/path",
					Description:  "test-description",
					Details: []KeyValue{
						{Key: "testkey", Value: "testvalue"},
					},
					RoutingKey: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "rksecret"},
						Key:                  "rkey",
					},
					Severity: "critical",
					URL:      "https://example.com",
				}},
				Webhooks: []WebhookReceiver{{
					SendResolved: true,
					URL:          "https://example.com",
				}},
				Emails: []EmailReceiver{{
					SendResolved: true,
					To:           "sre@example.com",
					From:         "alertmanager@example.com",
					Hello:        "example.com",
					Smarthost:    "smtp.example.com:587",
					AuthUsername: "alertmanager",
					AuthPassword: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "psecret"},
						Key:                  "pkey",
					},
					AuthSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "ssecret"},
						Key:                  "skey",
					},
					AuthIdentity: "something",
					HTML:         "<html><body>hello, world!</body></html>",
					Text:         "An alert is firing!",
					Headers: []KeyValue{{
						Key:   "headerkey",
						Value: "headervalue",
					}},
					RequireTLS: &_false,
				}},
			},
			Meta: metav1.ObjectMeta{Namespace: "testns", Name: "testn"},
			Expect: &alertmanagerconfig.Receiver{
				Name: "testns-testn-test",
				PagerdutyConfigs: []*alertmanagerconfig.PagerdutyConfig{{
					NotifierConfig: alertmanagerconfig.NotifierConfig{VSendResolved: true},
					Client:         "test-client",
					ClientURL:      "https://example.com/v1/path",
					Description:    "test-description",
					Details:        map[string]string{"testkey": "testvalue"},
					RoutingKey:     "defaultvalue",
					Severity:       "critical",
					URL:            "https://example.com",
				}},
				WebhookConfigs: []*alertmanagerconfig.WebhookConfig{{
					NotifierConfig: alertmanagerconfig.NotifierConfig{VSendResolved: true},
					URL:            "https://example.com",
				}},
				EmailConfigs: []*alertmanagerconfig.EmailConfig{{
					NotifierConfig: alertmanagerconfig.NotifierConfig{VSendResolved: true},
					To:             "sre@example.com",
					From:           "alertmanager@example.com",
					Hello:          "example.com",
					Smarthost:      "smtp.example.com:587",
					AuthUsername:   "alertmanager",
					AuthPassword:   "defaultvalue",
					AuthSecret:     "defaultvalue",
					AuthIdentity:   "something",
					Headers:        map[string]string{"headerkey": "headervalue"},
					HTML:           "<html><body>hello, world!</body></html>",
					Text:           "An alert is firing!",
					RequireTLS:     &_false,
				}},
			},
		},
	}

	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {

			// if no func passed, use a default
			getValueFromSecretKeySelector := s.SecretFunc
			if getValueFromSecretKeySelector == nil {
				getValueFromSecretKeySelector = func(namespace string, secretKeySelector *corev1.SecretKeySelector) (string, error) {
					return "defaultvalue", nil
				}
			}
			actual := s.Given.ToAMReceiver(s.Meta, getValueFromSecretKeySelector)

			if !cmp.Equal(actual, s.Expect) {
				t.Fatalf("Actual and expected Routes differ: %v", cmp.Diff(actual, s.Expect))
			}
		})
	}
}
