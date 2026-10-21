package main

import (
	"encoding/json"
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
