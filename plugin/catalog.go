package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// modelCapability records the endpoints a known AgentRouter model accepts.
// AgentRouter publishes a per-model endpoint list on /api/pricing; this table
// is the offline fallback used before the live catalogue has been fetched.
type modelCapability struct {
	// Protocols lists supported protocols in preference order.
	Protocols []string
	// ContextLength is the advertised context window.
	ContextLength int64
	// MaxCompletionTokens is the advertised output limit.
	MaxCompletionTokens int64
	// InputModalities lists accepted input types (text, image).
	InputModalities []string
}

var textOnly = []string{"text"}
var textAndImage = []string{"text", "image"}

var knownModelCapabilities = map[string]modelCapability{
	"deepseek-v4-flash": {Protocols: []string{protocolOpenAI, protocolAnthropic}, ContextLength: 1_000_000, MaxCompletionTokens: 256_000, InputModalities: textAndImage},
	"gpt-6-astra":       {Protocols: []string{protocolResponses}, ContextLength: 272_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"gpt-6.1-sol":       {Protocols: []string{protocolResponses}, ContextLength: 272_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"gpt-5.6-sol":       {Protocols: []string{protocolResponses}, ContextLength: 272_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"gpt-6-sol":         {Protocols: []string{protocolResponses}, ContextLength: 272_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"gpt-6-luna":        {Protocols: []string{protocolResponses}, ContextLength: 272_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"claude-opus-5":     {Protocols: []string{protocolAnthropic, protocolOpenAI}, ContextLength: 1_000_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"claude-opus-5-5":   {Protocols: []string{protocolAnthropic, protocolOpenAI}, ContextLength: 1_000_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"claude-opus-4-8":   {Protocols: []string{protocolAnthropic, protocolOpenAI}, ContextLength: 1_000_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
	"glm-5.3":           {Protocols: []string{protocolOpenAI, protocolAnthropic}, ContextLength: 200_000, MaxCompletionTokens: 128_000, InputModalities: textAndImage},
}

// catalogueEntry is the cached result of a live model discovery.
type catalogueEntry struct {
	Name      string   `json:"name"`
	Protocols []string `json:"protocols"`
	FetchedAt int64    `json:"fetched_at"`
}

type catalogueState struct {
	mu       sync.RWMutex
	models   []pluginapi.ModelInfo
	byName   map[string]modelCapability
	lastSync time.Time
	lastErr  string
	source   string
}

var catalogue catalogueState

// pricingEntry matches the AgentRouter /api/pricing payload.
type pricingEntry struct {
	ModelName       string   `json:"model_name"`
	EndpointTypes   []string `json:"supported_endpoint_types"`
	EnableGroups    []string `json:"enable_groups"`
	QuotaType       int      `json:"quota_type"`
	ModelRatio      float64  `json:"model_ratio"`
	CompletionRatio float64  `json:"completion_ratio"`
}

type pricingResponse struct {
	Data    []pricingEntry `json:"data"`
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Groups  map[string]any `json:"group_ratio"`
}

type apiModelEntry struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	OwnedBy       string   `json:"owned_by"`
	EndpointTypes []string `json:"supported_endpoint_types"`
}

type apiModelsResponse struct {
	Data    []apiModelEntry `json:"data"`
	Success bool            `json:"success"`
	Message string          `json:"message"`
}

// modelRegister returns the registered model catalogue to the host.
func modelRegister() ([]byte, error) {
	cfg := loadedConfig()
	models := resolveModels(cfg)
	published := make([]pluginapi.ModelInfo, 0, len(models))
	for _, spec := range models {
		published = append(published, modelInfoFor(spec))
	}
	return okEnvelope(pluginapi.ModelRegistrationResponse{
		Provider: providerKey,
		Models:   published,
	})
}

// resolveModels merges the configured models, the live catalogue and the
// built-in capabilities table into the final resolved list.
func resolveModels(cfg pluginConfig) []modelSpec {
	catalogue.mu.RLock()
	discovered := append([]pluginapi.ModelInfo(nil), catalogue.models...)
	catalogue.mu.RUnlock()

	byAlias := make(map[string]modelSpec)
	order := make([]string, 0, len(discovered)+len(cfg.Models))
	add := func(spec modelSpec) {
		key := modelClientName(spec)
		if key == "" {
			return
		}
		if existing, ok := byAlias[key]; ok {
			// Configured entries win over discovered entries; merge missing fields.
			if existing.ContextLength == 0 {
				existing.ContextLength = spec.ContextLength
			}
			if len(existing.Protocols) == 0 {
				existing.Protocols = spec.Protocols
			}
			if strings.TrimSpace(existing.DisplayName) == "" {
				existing.DisplayName = spec.DisplayName
			}
			byAlias[key] = existing
			return
		}
		byAlias[key] = spec
		order = append(order, key)
	}

	for _, item := range cfg.Models {
		add(item)
	}
	for _, item := range discovered {
		capability := capabilityForModel(item.ID)
		add(modelSpec{
			Name:                item.ID,
			DisplayName:         item.DisplayName,
			ContextLength:       item.ContextLength,
			MaxCompletionTokens: item.MaxCompletionTokens,
			Protocols:           capability.Protocols,
		})
	}
	if len(order) == 0 {
		// Nothing configured and discovery disabled: publish the capability
		// table so the host has a usable catalogue.
		for name := range knownModelCapabilities {
			order = append(order, name)
		}
		sort.Strings(order)
		for _, name := range order {
			byAlias[name] = modelSpec{Name: name, Protocols: capabilityForModel(name).Protocols}
		}
	}

	out := make([]modelSpec, 0, len(order))
	for _, key := range order {
		spec := byAlias[key]
		capability := capabilityForModel(spec.Name)
		if len(spec.Protocols) == 0 {
			spec.Protocols = capability.Protocols
		}
		if spec.ContextLength <= 0 {
			spec.ContextLength = capability.ContextLength
		}
		if spec.MaxCompletionTokens <= 0 {
			spec.MaxCompletionTokens = capability.MaxCompletionTokens
		}
		out = append(out, spec)
	}
	return out
}

func modelClientName(spec modelSpec) string {
	if strings.TrimSpace(spec.Alias) != "" {
		return strings.TrimSpace(spec.Alias)
	}
	return strings.TrimSpace(spec.Name)
}

// modelInfoFor converts a resolved model spec into the host model metadata.
func modelInfoFor(spec modelSpec) pluginapi.ModelInfo {
	display := spec.DisplayName
	if display == "" {
		display = spec.Name
	}
	modalities := inputModalitiesFor(spec)
	return pluginapi.ModelInfo{
		ID:                         modelClientName(spec),
		Object:                     "model",
		Created:                    0,
		OwnedBy:                    providerKey,
		Type:                       providerKey,
		DisplayName:                display,
		Name:                       spec.Name,
		ContextLength:              spec.ContextLength,
		MaxCompletionTokens:        spec.MaxCompletionTokens,
		InputTokenLimit:            spec.ContextLength,
		OutputTokenLimit:           spec.MaxCompletionTokens,
		SupportedGenerationMethods: []string{"chat", "responses", "messages"},
		SupportedInputModalities:   modalities,
		SupportedOutputModalities:  []string{"text"},
		UserDefined:                true,
	}
}

// inputModalitiesFor returns the advertised input modalities for a model.
func inputModalitiesFor(spec modelSpec) []string {
	if len(spec.InputModalities) > 0 {
		return spec.InputModalities
	}
	capability := capabilityForModel(spec.Name)
	if len(capability.InputModalities) > 0 {
		return capability.InputModalities
	}
	return textOnly
}

func capabilityForModel(name string) modelCapability {
	name = strings.ToLower(strings.TrimSpace(name))
	catalogue.mu.RLock()
	if capability, ok := catalogue.byName[name]; ok {
		catalogue.mu.RUnlock()
		return capability
	}
	catalogue.mu.RUnlock()
	if capability, ok := knownModelCapabilities[name]; ok {
		return capability
	}
	return inferCapability(name)
}

// inferCapability derives a conservative capability record for unknown models.
func inferCapability(name string) modelCapability {
	lower := strings.ToLower(name)
	protocols := []string{protocolOpenAI}
	switch {
	case strings.Contains(lower, "gpt"), strings.Contains(lower, "codex"), strings.Contains(lower, "o1"), strings.Contains(lower, "o3"):
		protocols = []string{protocolResponses}
	case strings.Contains(lower, "claude"), strings.Contains(lower, "opus"), strings.Contains(lower, "sonnet"):
		protocols = []string{protocolAnthropic, protocolOpenAI}
	case strings.Contains(lower, "deepseek"), strings.Contains(lower, "glm"), strings.Contains(lower, "kimi"), strings.Contains(lower, "qwen"):
		protocols = []string{protocolOpenAI, protocolAnthropic}
	}
	contextLength := int64(200_000)
	if strings.Contains(lower, "deepseek") || strings.Contains(lower, "opus") || strings.Contains(lower, "gpt-6") || strings.Contains(lower, "1m") {
		contextLength = 1_000_000
	}
	modalities := textOnly
	if strings.Contains(lower, "claude") || strings.Contains(lower, "gpt") || strings.Contains(lower, "gemini") ||
		strings.Contains(lower, "glm") || strings.Contains(lower, "qwen") || strings.Contains(lower, "kimi") ||
		strings.Contains(lower, "deepseek") || strings.Contains(lower, "vision") || strings.Contains(lower, "multimodal") {
		modalities = textAndImage
	}
	return modelCapability{
		Protocols:           protocols,
		ContextLength:       contextLength,
		MaxCompletionTokens: 32_768,
		InputModalities:     modalities,
	}
}

// protocolForModel returns the upstream protocol for a model, honouring the
// explicit override, then the model capability table, then a sane default.
func protocolForModel(cfg pluginConfig, model string) string {
	if protocol := normalizeProtocolName(cfg.ModelProtocols[model]); protocol != "" {
		return protocol
	}
	if protocol := normalizeProtocolName(cfg.ModelProtocols[strings.ToLower(model)]); protocol != "" {
		return protocol
	}
	capability := capabilityForModel(model)
	for _, protocol := range capability.Protocols {
		switch protocol {
		case protocolResponses, protocolAnthropic, protocolOpenAI:
			return protocol
		}
	}
	return protocolOpenAI
}

// protocolForRequest picks the wire protocol for a request, considering the
// caller protocol when the model supports several endpoints.
func protocolForRequest(cfg pluginConfig, model, sourceFormat string) string {
	preferred := protocolForModel(cfg, model)
	capability := capabilityForModel(model)
	source := canonicalSourceFormat(sourceFormat)
	if source != "" && len(capability.Protocols) > 1 {
		for _, protocol := range capability.Protocols {
			if protocol == source {
				return source
			}
		}
	}
	return preferred
}

func canonicalSourceFormat(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "claude", "anthropic", "messages":
		return protocolAnthropic
	case "openai-response", "responses", "codex":
		return protocolResponses
	case "openai", "chat-completions", "chat_completions":
		return protocolOpenAI
	default:
		return ""
	}
}

// refreshCatalogue fetches the live model list for the API key and records
// capability metadata in the cache.
func refreshCatalogue(ctx context.Context, cfg pluginConfig) ([]pluginapi.ModelInfo, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("api_key is not configured")
	}
	models, errModels := fetchAPIKeyModels(ctx, cfg)
	if errModels != nil {
		catalogue.mu.Lock()
		catalogue.lastErr = errModels.Error()
		catalogue.mu.Unlock()
		return nil, errModels
	}
	pricing, _ := fetchPricing(ctx, cfg)
	capabilities := make(map[string]modelCapability, len(models))
	if pricing != nil {
		for _, entry := range pricing.Data {
			protocols := make([]string, 0, len(entry.EndpointTypes))
			for _, endpoint := range entry.EndpointTypes {
				if protocol := normalizeProtocolName(endpoint); protocol != "" {
					protocols = appendUniqueProtocol(protocols, protocol)
				}
			}
			if len(protocols) == 0 {
				continue
			}
			capabilities[strings.ToLower(entry.ModelName)] = modelCapability{Protocols: protocols}
		}
	}
	for _, item := range models {
		name := strings.ToLower(item.ID)
		protocols := capabilities[name].Protocols
		if known, okKnown := knownModelCapabilities[name]; okKnown {
			// AgentRouter's public endpoint metadata labels the Responses-only
			// GPT-6 family as "openai"; the curated table is authoritative for
			// known models, discovery only augments unknown ones.
			protocols = append([]string(nil), known.Protocols...)
		}
		if len(protocols) == 0 {
			protocols = inferCapability(item.ID).Protocols
		}
		base := capabilityForModel(item.ID)
		capabilities[name] = modelCapability{
			Protocols:           protocols,
			ContextLength:       base.ContextLength,
			MaxCompletionTokens: base.MaxCompletionTokens,
			InputModalities:     base.InputModalities,
		}
	}
	now := time.Now()
	catalogue.mu.Lock()
	catalogue.models = models
	catalogue.byName = capabilities
	catalogue.lastSync = now
	catalogue.lastErr = ""
	catalogue.mu.Unlock()
	return models, nil
}

func appendUniqueProtocol(protocols []string, protocol string) []string {
	for _, existing := range protocols {
		if existing == protocol {
			return protocols
		}
	}
	return append(protocols, protocol)
}

func fetchAPIKeyModels(ctx context.Context, cfg pluginConfig) ([]pluginapi.ModelInfo, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	url := strings.TrimSuffix(cfg.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent)
	client := controlPlaneClient(cfg)
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, fmt.Errorf("fetch agentrouter models: %w", errDo)
	}
	defer resp.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if errRead != nil {
		return nil, fmt.Errorf("read agentrouter models: %w", errRead)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newUpstreamError(resp.StatusCode, summarizeUpstreamError(body))
	}
	var parsed apiModelsResponse
	if errUnmarshal := json.Unmarshal(body, &parsed); errUnmarshal != nil {
		return nil, fmt.Errorf("decode agentrouter models: %w", errUnmarshal)
	}
	if !parsed.Success && parsed.Message != "" {
		return nil, fmt.Errorf("agentrouter models: %s", parsed.Message)
	}
	out := make([]pluginapi.ModelInfo, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		capability := capabilityForModel(id)
		out = append(out, pluginapi.ModelInfo{
			ID:                         id,
			Object:                     "model",
			OwnedBy:                    providerKey,
			Type:                       providerKey,
			DisplayName:                id,
			Name:                       id,
			ContextLength:              capability.ContextLength,
			MaxCompletionTokens:        capability.MaxCompletionTokens,
			SupportedInputModalities:   capability.InputModalities,
			SupportedOutputModalities:  []string{"text"},
			SupportedGenerationMethods: []string{"chat", "responses", "messages"},
			UserDefined:                true,
		})
	}
	return out, nil
}

// fetchPricing reads the public pricing endpoint to learn which endpoint types
// each model supports (and its availability groups).
func fetchPricing(ctx context.Context, cfg pluginConfig) (*pricingResponse, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url := strings.TrimSuffix(cfg.BaseURL, "/") + "/api/pricing"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent)
	client := controlPlaneClient(cfg)
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer resp.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if errRead != nil {
		return nil, errRead
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newUpstreamError(resp.StatusCode, summarizeUpstreamError(body))
	}
	var parsed pricingResponse
	if errUnmarshal := json.Unmarshal(body, &parsed); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	return &parsed, nil
}

// summarizeUpstreamError extracts a human readable message from an upstream
// error body, falling back to the raw payload when it is not JSON.
func summarizeUpstreamError(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal == nil {
		if inner, ok := payload["error"].(map[string]any); ok {
			if message, okMessage := inner["message"].(string); okMessage && strings.TrimSpace(message) != "" {
				return message
			}
		}
		if message, okMessage := payload["message"].(string); okMessage && strings.TrimSpace(message) != "" {
			return message
		}
	}
	text := string(body)
	if len(text) > 2000 {
		text = text[:2000]
	}
	return text
}

// catalogueSnapshot returns a copy of the cached live catalogue state for the
// management UI.
func catalogueSnapshot() ([]pluginapi.ModelInfo, time.Time, string) {
	catalogue.mu.RLock()
	defer catalogue.mu.RUnlock()
	return append([]pluginapi.ModelInfo(nil), catalogue.models...), catalogue.lastSync, catalogue.lastErr
}

// onConfigLoaded is invoked whenever the plugin configuration loads or
// reloads. It primes the live model catalogue so the models published during
// registration match what the API key can actually use, and starts the
// optional scheduled check-in.
func onConfigLoaded(cfg pluginConfig) {
	primeCatalogue(cfg)
	checkinState.configure(cfg)
	if cfg.CheckinOnStartup {
		go func() {
			_, _ = performCheckin(context.Background(), cfg)
		}()
	}
}

// primeCatalogue performs a bounded synchronous discovery. The host asks for
// the model list during registration, so discovery must finish before the
// registration response is returned to publish accurate models.
func primeCatalogue(cfg pluginConfig) {
	if !autoDiscoverModels(cfg) || strings.TrimSpace(cfg.APIKey) == "" {
		return
	}
	catalogue.mu.RLock()
	lastSync := catalogue.lastSync
	catalogue.mu.RUnlock()
	if !lastSync.IsZero() && time.Since(lastSync) < 5*time.Minute {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if _, errRefresh := refreshCatalogue(ctx, cfg); errRefresh != nil {
		hostLog("warn", "agentrouter model discovery failed", map[string]any{"error": errRefresh.Error()})
	}
}
