package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestMergeOverridesKeepsStoredSecrets(t *testing.T) {
	cfg := defaultConfig()
	cfg.APIKey = "stored"
	cfg.Username = "user"
	cfg.Password = "pass"
	out := mergeOverrides(cfg, []byte(`{"api_key":"","username":"","password":"newpass"}`))
	if out.APIKey != "stored" || out.Username != "user" {
		t.Fatalf("stored credentials overridden: %#v", out)
	}
	if out.Password != "newpass" {
		t.Fatalf("password override ignored: %q", out.Password)
	}
}

func TestNormalizeManagementPath(t *testing.T) {
	cases := map[string]string{
		"/v0/resource/plugins/agentrouter/status":    resourceDashboardPath,
		"/v0/resource/plugins/agentrouter/":          resourceDashboardPath,
		"/v0/management/agentrouter/models":          routeModels,
		"/v0/management/agentrouter/refresh-models/": routeRefreshModels,
	}
	for input, want := range cases {
		if got := normalizeManagementPath(input); got != want {
			t.Fatalf("normalizeManagementPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestManagementRegistrationDeclaresDashboard(t *testing.T) {
	reg := managementRegistration()
	if len(reg.Resources) != 1 || reg.Resources[0].Menu != "AgentRouter" {
		t.Fatalf("resources = %#v", reg.Resources)
	}
	if len(reg.Routes) < 4 {
		t.Fatalf("routes = %#v", reg.Routes)
	}
}

func TestHandleManagementModels(t *testing.T) {
	configureTestPlugin(t)
	raw, err := json.Marshal(pluginapi.ManagementRequest{Method: "GET", Path: "/v0/management/agentrouter/models"})
	if err != nil {
		t.Fatal(err)
	}
	resp, errHandle := handleManagement(raw)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(resp, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if !env.OK {
		t.Fatalf("envelope = %s", resp)
	}
	var response pluginapi.ManagementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if response.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", response.StatusCode, response.Body)
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if errUnmarshal := json.Unmarshal(response.Body, &payload); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if len(payload.Models) == 0 {
		t.Fatal("expected models in response")
	}
}

func TestModelRegisterPublishesCuratedProtocols(t *testing.T) {
	configureTestPlugin(t)
	resp, errRegister := modelRegister()
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(resp, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	var registrationResponse pluginapi.ModelRegistrationResponse
	if errUnmarshal := json.Unmarshal(env.Result, &registrationResponse); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if registrationResponse.Provider != providerKey || len(registrationResponse.Models) == 0 {
		t.Fatalf("registration = %#v", registrationResponse)
	}
}

func configureTestPlugin(t *testing.T) {
	t.Helper()
	if errConfigure := configure(nil); errConfigure != nil {
		t.Fatal(errConfigure)
	}
	activeConfigMu.Lock()
	cfg := activeConfig
	cfg.AutoDiscoverModels = boolPointer(false)
	cfg.Models = nil
	activeConfig = cfg
	activeConfigMu.Unlock()
}

func boolPointer(value bool) *bool { return &value }

func TestSaveSettingsPersistsAndMerges(t *testing.T) {
	configureTestPlugin(t)
	cfg := loadedConfig()
	cfg.DataDir = t.TempDir()
	pluginStore.mu.Lock()
	pluginStore.path = statePath(cfg)
	pluginStore.data = pluginState{}
	pluginStore.mu.Unlock()
	t.Cleanup(func() {
		pluginStore.mu.Lock()
		pluginStore.path = ""
		pluginStore.data = pluginState{}
		pluginStore.mu.Unlock()
	})
	activeConfigMu.Lock()
	activeConfig = cfg
	activeConfigMu.Unlock()
	body, _ := json.Marshal(map[string]any{
		"user_agent": "claude-cli/9.9.9 (external, cli)",
		"api_key":    "sk-dashboard",
	})
	raw, _ := json.Marshal(pluginapi.ManagementRequest{Method: "POST", Path: "/v0/management/agentrouter/settings", Body: body})
	resp, errHandle := handleManagement(raw)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(resp, &env); errUnmarshal != nil || !env.OK {
		t.Fatalf("envelope = %s err=%v", resp, errUnmarshal)
	}
	merged := effectiveConfig()
	if merged.APIKey != "sk-dashboard" || merged.UserAgent != "claude-cli/9.9.9 (external, cli)" {
		t.Fatalf("merged config = %#v", merged)
	}
	if state := cachedState(); state.APIKey != "sk-dashboard" {
		t.Fatalf("state = %#v", state)
	}
}

func TestAuthProviderDeclaresCapabilityAndIdentity(t *testing.T) {
	reg := buildRegistration()
	if !reg.Capabilities.AuthProvider {
		t.Fatal("auth provider capability missing")
	}
	raw, errHandle := handleMethod("auth.identifier", nil)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil || !env.OK {
		t.Fatalf("envelope = %s", raw)
	}
	var identifier identifierResponse
	if errUnmarshal := json.Unmarshal(env.Result, &identifier); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if identifier.Identifier != providerKey {
		t.Fatalf("identifier = %q", identifier.Identifier)
	}
}

func TestParseAuthClaimsOnlyOwnFiles(t *testing.T) {
	raw, _ := json.Marshal(pluginapi.AuthParseRequest{FileName: "agentrouter.json", RawJSON: []byte(`{"type":"agentrouter","api_key":"k","username":"u"}`)})
	resp, errHandle := parseAuth(raw)
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	_ = json.Unmarshal(resp, &env)
	var parsed pluginapi.AuthParseResponse
	if errUnmarshal := json.Unmarshal(env.Result, &parsed); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if !parsed.Handled || parsed.Auth.Provider != providerKey {
		t.Fatalf("parsed = %#v", parsed)
	}
	raw, _ = json.Marshal(pluginapi.AuthParseRequest{FileName: "other.json", RawJSON: []byte(`{"type":"gemini-cli"}`)})
	resp, _ = parseAuth(raw)
	_ = json.Unmarshal(resp, &env)
	_ = json.Unmarshal(env.Result, &parsed)
	if parsed.Handled {
		t.Fatal("must not claim foreign auth files")
	}
}

func TestLoginPageRendersState(t *testing.T) {
	page := renderLoginPage("agentrouter-test")
	if !strings.Contains(page, "agentrouter-test") || !strings.Contains(page, "系统令牌") {
		t.Fatal("login page missing state or token form")
	}
}

func TestFetchAnnouncementsShape(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	if items, err := fetchAnnouncements(context.Background(), cfg); err == nil && len(items) == 0 {
		t.Fatal("expected announcements")
	}
}
