// Package main implements the AgentRouter provider plugin for CLIProxyAPI /
// EasyCLIProxyAPI.
//
// The plugin is built as a native dynamic library (C ABI) and registers three
// capabilities with the host:
//
//   - model_registrar: publishes the models configured for AgentRouter.
//   - executor: speaks the AgentRouter upstream protocols (OpenAI Chat
//     Completions, OpenAI Responses and Anthropic Messages).
//   - auth_provider: the plugin card's 账号授权 button opens a local sign-in
//     page that captures the AgentRouter session/access token.
//   - management_api: exposes a browser console page with the account
//     overview, usage dashboard, model catalogue, usage log, check-in and
//     editable settings.
//
// AgentRouter gates its gateway on the client User-Agent, so every upstream
// request is sent with a configurable, client-like User-Agent.
package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	pluginName    = "agentrouter"
	pluginVersion = "0.2.1"
	pluginAuthor  = "smartwu2003"
	pluginRepo    = "https://github.com/smartwu2003/agentrouter-cliproxyapi-plugin"
	providerKey   = "agentrouter"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type lifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	ModelRegistrar bool     `json:"model_registrar"`
	ModelRouter    bool     `json:"model_router"`
	Executor       bool     `json:"executor"`
	AuthProvider   bool     `json:"auth_provider"`
	ManagementAPI  bool     `json:"management_api"`
	ExecutorScope  string   `json:"executor_model_scope"`
	InputFormats   []string `json:"executor_input_formats,omitempty"`
	OutputFormats  []string `json:"executor_output_formats,omitempty"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

type managementRegistrationResponse struct {
	Routes    []pluginapi.ManagementRoute `json:"routes,omitempty"`
	Resources []pluginapi.ResourceRoute   `json:"resources,omitempty"`
}

var (
	activeConfigMu sync.RWMutex
	activeConfig   = defaultConfig()
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	if host != nil {
		// storeHostAPI is defined in host.go; the stored pointer lets plugin
		// code issue host callbacks.
		storeHostAPI(host)
	}
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required", 0))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		raw = errorEnvelopeFromError(errHandle)
		writeResponse(response, raw)
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(buildRegistration())
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		return okEnvelope(map[string]any{})
	case pluginabi.MethodModelRegister:
		return modelRegister()
	case pluginabi.MethodModelRoute:
		return routeModelRequest(request)
	case pluginabi.MethodExecutorIdentifier, pluginabi.MethodAuthIdentifier:
		return okEnvelope(identifierResponse{Identifier: providerKey})
	case pluginabi.MethodAuthParse:
		return parseAuth(request)
	case pluginabi.MethodAuthLoginStart:
		return startLogin(request)
	case pluginabi.MethodAuthLoginPoll:
		return pollLogin(request)
	case pluginabi.MethodAuthRefresh:
		return refreshAuth(request)
	case pluginabi.MethodExecutorExecute:
		return executeRequest(request)
	case pluginabi.MethodExecutorExecuteStream:
		return executeStreamRequest(request)
	case pluginabi.MethodExecutorCountTokens:
		return okEnvelope(pluginapi.ExecutorResponse{Payload: []byte(`{"total_tokens":0}`)})
	case pluginabi.MethodExecutorHTTPRequest:
		return errorEnvelope("not_supported", "http_request is not supported", 0), nil
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration())
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method, 0), nil
	}
}

func buildRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           pluginAuthor,
			GitHubRepository: pluginRepo,
			ConfigFields: []pluginapi.ConfigField{
				{Name: "base_url", Type: pluginapi.ConfigFieldTypeString, Description: "AgentRouter API base URL. Use https://ps.air-outer.com (direct, China) or https://agentrouter.org (needs a proxy)."},
				{Name: "api_key", Type: pluginapi.ConfigFieldTypeString, Description: "AgentRouter API key (Bearer token). Read from the AgentRouter console."},
				{Name: "user_agent", Type: pluginapi.ConfigFieldTypeString, Description: "Client User-Agent sent upstream. AgentRouter rejects unrecognised clients; keep a whitelisted client identity."},
				{Name: "proxy_url", Type: pluginapi.ConfigFieldTypeString, Description: "Optional HTTP/SOCKS5 proxy used only for AgentRouter control-plane calls (catalogue refresh, account, check-in)."},
				{Name: "models", Type: pluginapi.ConfigFieldTypeArray, Description: "Explicit model list. Each entry is an object with name, alias, display_name, context_length, max_completion_tokens and protocols fields."},
				{Name: "auto_discover_models", Type: pluginapi.ConfigFieldTypeBoolean, Default: true, Description: "Fetch the models the API key can use from AgentRouter at startup and on refresh."},
				{Name: "model_protocols", Type: pluginapi.ConfigFieldTypeObject, Description: "Map of model name to protocol override (openai, responses or anthropic)."},
				{Name: "username", Type: pluginapi.ConfigFieldTypeString, Description: "AgentRouter account username or email, used for the daily check-in."},
				{Name: "password", Type: pluginapi.ConfigFieldTypeString, Description: "AgentRouter account password, used for the daily check-in."},
				{Name: "account_token", Type: pluginapi.ConfigFieldTypeString, Description: "Optional New API system access token (个人设置 → 系统令牌). The dashboard stores it automatically after an account authorization."},
				{Name: "user_id", Type: pluginapi.ConfigFieldTypeString, Description: "Optional numeric AgentRouter user ID sent as New-Api-User for dashboard calls."},
				{Name: "api_key_name", Type: pluginapi.ConfigFieldTypeString, Default: "EasyCLIProxyAPI", Description: "Token name the plugin creates or reuses when provisioning an inference API key."},
				{Name: "data_dir", Type: pluginapi.ConfigFieldTypeString, Description: "Directory for persisted dashboard state (default: plugins/ next to the core binary)."},
				{Name: "dashboard_base_url", Type: pluginapi.ConfigFieldTypeString, Default: defaultDashboardURL, Description: "Console origin used for account/dashboard APIs (default https://ps.air-outer.com; the plugin retries https://agentrouter.org when the WAF blocks it)."},
				{Name: "checkin_on_startup", Type: pluginapi.ConfigFieldTypeBoolean, Default: false, Description: "Run the daily check-in once when the plugin loads."},
				{Name: "checkin_hour", Type: pluginapi.ConfigFieldTypeInteger, Default: 9, Description: "Beijing-time hour (0-23) after which the automatic check-in may run."},
			},
		},
		Capabilities: registrationCapability{
			ModelRegistrar: true,
			ModelRouter:    true,
			Executor:       true,
			AuthProvider:   true,
			ManagementAPI:  true,
			ExecutorScope:  string(pluginapi.ExecutorModelScopeStatic),
			InputFormats:   []string{"chat-completions", "responses", "anthropic"},
			OutputFormats:  []string{"chat-completions", "responses", "anthropic"},
		},
	}
}

func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return fmt.Errorf("decode plugin lifecycle request: %w", errUnmarshal)
		}
	}
	cfg := defaultConfig()
	if len(req.ConfigYAML) > 0 {
		decoded, errDecode := decodeConfig(req.ConfigYAML)
		if errDecode != nil {
			return errDecode
		}
		cfg = decoded
	}
	// Dashboard-created credentials live in the plugin state file; merge them
	// so a reconfigure does not silently drop an account authorization.
	state := loadState(cfg)
	cfg = applyState(cfg, state)
	activeConfigMu.Lock()
	activeConfig = cfg
	activeConfigMu.Unlock()
	onConfigLoaded(cfg)
	return nil
}

func loadedConfig() pluginConfig {
	activeConfigMu.RLock()
	defer activeConfigMu.RUnlock()
	return activeConfig
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string, httpStatus int) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}})
	return raw
}

type statusCoder interface {
	StatusCode() int
}

func errorEnvelopeFromError(err error) []byte {
	if err == nil {
		return errorEnvelope("plugin_error", "unknown error", 0)
	}
	status := 0
	var sc statusCoder
	if errors.As(err, &sc) && sc != nil {
		status = sc.StatusCode()
	}
	code := "plugin_error"
	if status == http.StatusUnauthorized {
		code = "invalid_api_key"
	} else if status == http.StatusForbidden {
		code = "insufficient_quota"
	} else if status == http.StatusTooManyRequests {
		code = "rate_limit_exceeded"
	}
	return errorEnvelope(code, err.Error(), status)
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

// upstreamError carries an HTTP status code from an upstream failure so the
// host can map it onto a structured client error.
type upstreamError struct {
	Status  int
	Message string
}

func (e *upstreamError) Error() string { return e.Message }

func (e *upstreamError) StatusCode() int { return e.Status }

func newUpstreamError(status int, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 2000 {
		message = message[:2000]
	}
	if message == "" {
		message = fmt.Sprintf("AgentRouter upstream returned HTTP %d", status)
	}
	return &upstreamError{Status: status, Message: message}
}
