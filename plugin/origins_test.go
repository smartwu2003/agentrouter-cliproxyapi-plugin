package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// resetOrigins clears the remembered/penalized origins between tests.
func resetOrigins(t *testing.T) {
	t.Helper()
	inferenceOriginMemory.reset()
	consoleOriginMemory.reset()
	previous := alternateOriginTestHook
	alternateOriginTestHook = ""
	t.Cleanup(func() {
		inferenceOriginMemory.reset()
		consoleOriginMemory.reset()
		alternateOriginTestHook = previous
	})
}

func TestAlternateOriginPicksTheOtherSite(t *testing.T) {
	previous := alternateOriginTestHook
	alternateOriginTestHook = ""
	t.Cleanup(func() { alternateOriginTestHook = previous })

	cases := map[string]string{
		mainlandOrigin:               globalOrigin,
		globalOrigin:                 mainlandOrigin,
		"https://co.agentrouter.org": mainlandOrigin,
		"https://ps.air-outer.com/":  globalOrigin,
	}
	for origin, want := range cases {
		if got := alternateOrigin(origin); got != want {
			t.Fatalf("alternateOrigin(%q) = %q, want %q", origin, got, want)
		}
	}
	if !originNeedsProxy(globalOrigin) {
		t.Fatal("agentrouter.org must be treated as proxy-only")
	}
	if originNeedsProxy(mainlandOrigin) {
		t.Fatal("ps.air-outer.com must be treated as direct")
	}
}

func TestProxyOnlyAppliesToTheProxiedOrigin(t *testing.T) {
	cfg := defaultConfig()
	cfg.ProxyURL = "http://127.0.0.1:7897"
	if got := proxyForOrigin(cfg, mainlandOrigin); got != "" {
		t.Fatalf("mainland origin must not use the proxy, got %q", got)
	}
	if got := proxyForOrigin(cfg, globalOrigin); got != cfg.ProxyURL {
		t.Fatalf("agentrouter.org must use the proxy, got %q", got)
	}
}

func TestOriginCandidatesOrderAndPenalty(t *testing.T) {
	resetOrigins(t)
	memory := &originMemory{}
	if got := memory.candidates(mainlandOrigin); len(got) != 2 || got[0] != mainlandOrigin || got[1] != globalOrigin {
		t.Fatalf("candidates = %v", got)
	}
	memory.remember(mainlandOrigin)
	if got := memory.candidates(mainlandOrigin); got[0] != mainlandOrigin {
		t.Fatalf("remembered origin should come first: %v", got)
	}
	memory.markFailed(mainlandOrigin)
	got := memory.candidates(mainlandOrigin)
	if len(got) != 2 || got[0] != globalOrigin {
		t.Fatalf("penalized origin should move to the end: %v", got)
	}
	memory.remember(mainlandOrigin)
	if memory.penalized(mainlandOrigin, time.Now()) {
		t.Fatal("remembering a working origin must clear its penalty")
	}
}

func TestConsoleFailsOverToAlternateOriginOnWAF(t *testing.T) {
	resetOrigins(t)
	waf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><meta name=\"aliyun_waf_aa\" content=\"x\">"))
	}))
	defer waf.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":706609,"username":"github_706609","group":"default","quota":500000}}`))
	}))
	defer good.Close()
	alternateOriginTestHook = good.URL

	cfg := defaultConfig()
	cfg.DashboardBaseURL = waf.URL
	client := accountClient{cfg: cfg, token: "token", userID: "706609"}
	user, errSelf := client.fetchSelf(context.Background())
	if errSelf != nil {
		t.Fatalf("console did not fail over: %v", errSelf)
	}
	if user.ID != 706609 || user.Group != "default" {
		t.Fatalf("user = %+v", user)
	}
	if got := consoleOriginMemory.get(); got != normalizeBaseURL(good.URL) {
		t.Fatalf("console origin = %q, want %q", got, good.URL)
	}
}

func TestInferenceFailsOverToAlternateOriginOnTransportError(t *testing.T) {
	resetOrigins(t)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"content":[{"type":"output_text","text":"OK"}]}]}`))
	}))
	defer good.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // the port is now closed: connecting fails before any answer
	alternateOriginTestHook = good.URL

	cfg := defaultConfig()
	cfg.APIKey = "sk-test"
	cfg.BaseURL = deadURL
	req := pluginapi.ExecutorRequest{
		Model:        "gpt-6.1-sol",
		SourceFormat: "openai",
		Payload:      []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}]}`),
	}
	resp, errRun := runUpstream(context.Background(), cfg, req, false)
	if errRun != nil {
		t.Fatalf("inference did not fail over: %v", errRun)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "OK") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if got := inferenceOriginMemory.get(); got != normalizeBaseURL(good.URL) {
		t.Fatalf("inference origin = %q, want %q", got, good.URL)
	}
}

func TestAccountCardKeepsKeyAndCachedValuesWhenConsoleFails(t *testing.T) {
	resetOrigins(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"gpt-6.1-sol"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"message":"控制台繁忙"}`))
	}))
	defer server.Close()
	alternateOriginTestHook = server.URL

	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.APIKey = "sk-abcdefghijklmnop"
	cfg.BaseURL = server.URL
	cfg.DashboardBaseURL = server.URL
	if errSave := saveState(cfg, pluginState{Account: &storedAccount{
		Username:     "ghost",
		DisplayName:  "Ghost",
		UserID:       "706609",
		Group:        "vip",
		AccountToken: "token",
		Quota:        1_000_000,
		UsedQuota:    500_000,
		RequestCount: 12,
		LoginAt:      1792658433,
	}}); errSave != nil {
		t.Fatal(errSave)
	}

	info, errInfo := fetchAccountInfo(context.Background(), cfg)
	if errInfo != nil {
		t.Fatal(errInfo)
	}
	if info.APIKeyMasked == "" {
		t.Fatal("the masked inference key must survive a console failure")
	}
	if !info.Stale || info.Group != "vip" || info.UserID != "706609" {
		t.Fatalf("cached account values were not restored: %+v", info)
	}
	if info.QuotaUSD != 2 {
		t.Fatalf("cached quota = %v, want 2", info.QuotaUSD)
	}
	if !strings.Contains(info.Message, "缓存") {
		t.Fatalf("message = %q", info.Message)
	}
	if len(info.Models) != 1 || info.Models[0] != "gpt-6.1-sol" {
		t.Fatalf("models = %v", info.Models)
	}
}
