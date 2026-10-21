package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type rpcModelRouteRequest struct {
	pluginapi.ModelRouteRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// routeModelRequest answers the host model router. Requests for an AgentRouter
// model are executed by this plugin's own executor; everything else is left to
// the host's built-in providers.
func routeModelRequest(raw []byte) ([]byte, error) {
	var req rpcModelRouteRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode model route request: %w", errUnmarshal)
	}
	requested := strings.TrimSpace(req.RequestedModel)
	if requested == "" {
		requested = upstreamModelFromPayload(req.Body)
	}
	if requested == "" {
		return okEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}
	model := stripProviderPrefix(requested)
	cfg := loadedConfig()
	if !isOwnedModel(cfg, model) {
		return okEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return okEnvelope(pluginapi.ModelRouteResponse{
			Handled: false,
			Reason:  "agentrouter api_key is not configured",
		})
	}
	return okEnvelope(pluginapi.ModelRouteResponse{
		Handled:    true,
		TargetKind: pluginapi.ModelRouteTargetSelf,
		Reason:     "agentrouter model",
	})
}

// isOwnedModel reports whether the requested model belongs to this plugin.
func isOwnedModel(cfg pluginConfig, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, spec := range resolveModels(cfg) {
		if strings.ToLower(spec.Name) == model || strings.ToLower(modelClientName(spec)) == model {
			return true
		}
	}
	return false
}

// stripProviderPrefix removes a leading "<provider>/" segment from a model ID.
func stripProviderPrefix(model string) string {
	model = strings.TrimSpace(model)
	if index := strings.LastIndex(model, "/"); index >= 0 && index < len(model)-1 {
		prefix := strings.ToLower(strings.TrimSpace(model[:index]))
		if prefix == providerKey {
			return strings.TrimSpace(model[index+1:])
		}
	}
	return model
}
