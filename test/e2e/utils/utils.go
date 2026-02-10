package utils

import (
	"context"

	amconfig "github.com/prometheus/alertmanager/config"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
)

const (
	AlertmanagerMainSecret = "alertmanager-main"
	AlertmanagerConfigKey  = "alertmanager.yaml"
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
