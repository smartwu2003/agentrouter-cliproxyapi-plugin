package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// modelSpec describes one AgentRouter model. Protocol routing is resolved from
// the model capability table plus explicit configuration.
type modelSpec struct {
	// Name is the canonical model ID sent upstream.
	Name string `yaml:"name" json:"name"`
	// DisplayName is the client-facing label; defaults to Name.
	DisplayName string `yaml:"display_name" json:"display_name"`
	// ContextLength is the advertised context window.
	ContextLength int64 `yaml:"context_length" json:"context_length"`
	// MaxCompletionTokens is the advertised output limit.
	MaxCompletionTokens int64 `yaml:"max_completion_tokens" json:"max_completion_tokens"`
	// Protocols lists the AgentRouter endpoint types accepted by the model.
	Protocols []string `yaml:"protocols" json:"protocols"`
	// Alias overrides the client-visible model ID (defaults to Name).
	Alias string `yaml:"alias" json:"alias"`
	// InputModalities overrides advertised input types (text, image).
	InputModalities []string `yaml:"input_modalities" json:"input_modalities"`
}

// pluginConfig is the full plugin configuration parsed from plugins.configs.agentrouter.
type pluginConfig struct {
	// BaseURL is the AgentRouter API base URL (without /v1).
	BaseURL string `yaml:"base_url"`
	// APIKey authenticates inference and account calls.
	APIKey string `yaml:"api_key"`
	// UserAgent identifies the client to AgentRouter's gateway filter.
	UserAgent string `yaml:"user_agent"`
	// ProxyURL is an optional proxy for control-plane requests.
	ProxyURL string `yaml:"proxy_url"`
	// Models is the explicitly configured model list; merged with discovery.
	Models []modelSpec `yaml:"models"`
	// AutoDiscoverModels fetches the API-key model list at startup.
	AutoDiscoverModels *bool `yaml:"auto_discover_models"`
	// ModelProtocols overrides the protocol chosen for a model.
	ModelProtocols map[string]string `yaml:"model_protocols"`
	// Username is the AgentRouter account used for check-in.
	Username string `yaml:"username"`
	// Password is the AgentRouter account password used for check-in.
	Password string `yaml:"password"`
	// CheckinOnStartup runs a check-in attempt when the plugin is configured.
	CheckinOnStartup bool `yaml:"checkin_on_startup"`
	// CheckinHour is the Beijing hour gate for automatic check-ins.
	CheckinHour int `yaml:"checkin_hour"`
}

const (
	defaultBaseURL   = "https://ps.air-outer.com"
	defaultUserAgent = "claude-cli/2.1.161 (external, cli)"
)

func defaultConfig() pluginConfig {
	autoDiscover := true
	return pluginConfig{
		BaseURL:            defaultBaseURL,
		UserAgent:          defaultUserAgent,
		AutoDiscoverModels: &autoDiscover,
		CheckinHour:        9,
	}
}

// decodeConfig parses YAML plugin configuration on top of the defaults and
// normalizes every string field.
func decodeConfig(raw []byte) (pluginConfig, error) {
	cfg := defaultConfig()
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return defaultConfig(), fmt.Errorf("decode agentrouter plugin config: %w", errUnmarshal)
	}
	normalizeConfig(&cfg)
	return cfg, nil
}

func normalizeConfig(cfg *pluginConfig) {
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Password = strings.TrimSpace(cfg.Password)
	if cfg.CheckinHour < 0 || cfg.CheckinHour > 23 {
		cfg.CheckinHour = 9
	}
	normalizedProtocols := make(map[string]string, len(cfg.ModelProtocols))
	for name, protocol := range cfg.ModelProtocols {
		name = strings.TrimSpace(name)
		protocol = normalizeProtocolName(protocol)
		if name == "" || protocol == "" {
			continue
		}
		normalizedProtocols[name] = protocol
	}
	cfg.ModelProtocols = normalizedProtocols
	models := make([]modelSpec, 0, len(cfg.Models))
	for _, item := range cfg.Models {
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" {
			continue
		}
		item.DisplayName = strings.TrimSpace(item.DisplayName)
		item.Alias = strings.TrimSpace(item.Alias)
		protocols := make([]string, 0, len(item.Protocols))
		seen := make(map[string]struct{}, len(item.Protocols))
		for _, protocol := range item.Protocols {
			protocol = normalizeProtocolName(protocol)
			if protocol == "" {
				continue
			}
			if _, exists := seen[protocol]; exists {
				continue
			}
			seen[protocol] = struct{}{}
			protocols = append(protocols, protocol)
		}
		item.Protocols = protocols
		modalities := make([]string, 0, len(item.InputModalities))
		seenModalities := make(map[string]struct{}, len(item.InputModalities))
		for _, modality := range item.InputModalities {
			modality = strings.ToLower(strings.TrimSpace(modality))
			if modality != "text" && modality != "image" {
				continue
			}
			if _, exists := seenModalities[modality]; exists {
				continue
			}
			seenModalities[modality] = struct{}{}
			modalities = append(modalities, modality)
		}
		item.InputModalities = modalities
		models = append(models, item)
	}
	cfg.Models = models
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return defaultBaseURL
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	return raw
}

func normalizeProtocolName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return ""
	case "openai", "chat", "chat-completions", "chat_completions", "openai-chat":
		return protocolOpenAI
	case "responses", "response", "openai-responses", "openai_responses", "openai-response", "codex":
		return protocolResponses
	case "anthropic", "claude", "messages":
		return protocolAnthropic
	default:
		return ""
	}
}

const (
	protocolOpenAI    = "openai"
	protocolResponses = "responses"
	protocolAnthropic = "anthropic"
)

func autoDiscoverModels(cfg pluginConfig) bool {
	return cfg.AutoDiscoverModels == nil || *cfg.AutoDiscoverModels
}
