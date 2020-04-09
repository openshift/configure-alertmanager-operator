package alertmanagerconfig

import (
	"fmt"

	yaml "gopkg.in/yaml.v2"
)

// PDRegex is the regular expression used in the Pager Duty receiver.
// This is specific to our environment.
const PDRegex string = "^default$|^kube.*|^openshift.*|^redhat-.*"

// The following types are taken from the upstream Alertmanager types, and modified
// to allow printing of Secrets so that we can generate valid configs from them.
// The Alertmanager types are not supported as external libraries, and therefore need
// to be recreated for this operator.
// Discussion, for reference, is in this PR: https://github.com/prometheus/alertmanager/pull/1804

type Config struct {
	Global    *GlobalConfig `yaml:"global,omitempty" json:"global,omitempty"`
	Route     *Route        `yaml:"route,omitempty" json:"route,omitempty"`
	Receivers []*Receiver   `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	Templates []string      `yaml:"templates" json:"templates"`

	// original is the input from which the config was parsed.
	original string
}

func (c Config) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating config string: %s>", err)
	}
	return string(b)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Config.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if c.Global == nil {
		c.Global = &GlobalConfig{}
	}

	names := map[string]struct{}{}

	for _, rcv := range c.Receivers {
		if _, ok := names[rcv.Name]; ok {
			return fmt.Errorf("notification config name %q is not unique", rcv.Name)
		}
		for _, pdc := range rcv.PagerdutyConfigs {
			if pdc.URL == "" {
				if c.Global.PagerdutyURL == "" {
					// Set Global default for Pager Duty URL
					c.Global.PagerdutyURL = "https://events.pagerduty.com/v2/enqueue"
				}
			}
		}
		names[rcv.Name] = struct{}{}
	}
	return nil
}

// NotifierConfig contains base options common across all notifier configurations.
type NotifierConfig struct {
	VSendResolved bool `yaml:"send_resolved" json:"send_resolved"`
}

// GlobalConfig defines configuration parameters that are valid globally
// unless overwritten.
type GlobalConfig struct {
	// ResolveTimeout is the time after which an alert is declared resolved
	// if it has not been updated.
	ResolveTimeout string `yaml:"resolve_timeout" json:"resolve_timeout"`

	PagerdutyURL string `yaml:"pagerduty_url,omitempty" json:"pagerduty_url,omitempty"`
}

// A Route is a node that contains definitions of how to handle alerts.
type Route struct {
	Receiver string `yaml:"receiver,omitempty" json:"receiver,omitempty"`

	GroupByStr []string `yaml:"group_by,omitempty" json:"group_by,omitempty"`

	Match    map[string]string `yaml:"match,omitempty" json:"match,omitempty"`
	MatchRE  map[string]string `yaml:"match_re,omitempty" json:"match_re,omitempty"`
	Continue bool              `yaml:"continue,omitempty" json:"continue,omitempty"`
	Routes   []*Route          `yaml:"routes,omitempty" json:"routes,omitempty"`

	GroupWait      string `yaml:"group_wait,omitempty" json:"group_wait,omitempty"`
	GroupInterval  string `yaml:"group_interval,omitempty" json:"group_interval,omitempty"`
	RepeatInterval string `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"`
}

type Receiver struct {
	// A unique identifier for this receiver.
	Name string `yaml:"name" json:"name"`

	PagerdutyConfigs []*PagerdutyConfig `yaml:"pagerduty_configs,omitempty" json:"pagerduty_configs,omitempty"`
	WebhookConfigs   []*WebhookConfig   `yaml:"webhook_configs,omitempty" json:"webhook_configs,omitempty"`
}

// WebhookConfig configures notifications via a generic webhook.
type WebhookConfig struct {
	NotifierConfig `yaml:",inline" json:",inline"`

	// URL to send POST request to.
	URL string `yaml:"url" json:"url"`
}

type PagerdutyConfig struct {
	NotifierConfig `yaml:",inline" json:",inline"`

	RoutingKey  string            `yaml:"routing_key,omitempty" json:"routing_key,omitempty"`
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"`
	Client      string            `yaml:"client,omitempty" json:"client,omitempty"`
	ClientURL   string            `yaml:"client_url,omitempty" json:"client_url,omitempty"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Details     map[string]string `yaml:"details,omitempty" json:"details,omitempty"`
	Severity    string            `yaml:"severity,omitempty" json:"severity,omitempty"`
	Class       string            `yaml:"class,omitempty" json:"class,omitempty"`
	Component   string            `yaml:"component,omitempty" json:"component,omitempty"`
	Group       string            `yaml:"group,omitempty" json:"group,omitempty"`
}
