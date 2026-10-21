package main

import (
	"testing"
)

func TestProtocolForModelUsesCuratedTable(t *testing.T) {
	cfg := defaultConfig()
	cases := map[string]string{
		"deepseek-v4-flash": protocolOpenAI,
		"gpt-6-astra":       protocolResponses,
		"gpt-6.1-sol":       protocolResponses,
		"claude-opus-5":     protocolAnthropic,
	}
	for model, want := range cases {
		if got := protocolForModel(cfg, model); got != want {
			t.Fatalf("protocolForModel(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestProtocolOverrideWins(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelProtocols = map[string]string{"gpt-6-astra": "openai"}
	if got := protocolForModel(cfg, "gpt-6-astra"); got != protocolOpenAI {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestInferCapabilityUnknownModels(t *testing.T) {
	cases := map[string]string{
		"gpt-7-x":           protocolResponses,
		"claude-nova-1":     protocolAnthropic,
		"deepseek-v9":       protocolOpenAI,
		"something-unknown": protocolOpenAI,
	}
	for model, want := range cases {
		got := inferCapability(model)
		if len(got.Protocols) == 0 || got.Protocols[0] != want {
			t.Fatalf("inferCapability(%q) = %v, want %q", model, got.Protocols, want)
		}
	}
}

func TestNormalizeConfigBaseURLSeeds(t *testing.T) {
	cfg, err := decodeConfig([]byte("base_url: ps.air-outer.com/\nuser_agent: \"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://ps.air-outer.com" {
		t.Fatalf("base url = %q", cfg.BaseURL)
	}
	if cfg.UserAgent != defaultUserAgent {
		t.Fatalf("user agent = %q", cfg.UserAgent)
	}
	if !autoDiscoverModels(cfg) {
		t.Fatal("auto discovery should default on")
	}
}

func TestResolveModelsPrefersConfiguredAlias(t *testing.T) {
	cfg := defaultConfig()
	cfg.Models = []modelSpec{{Name: "deepseek-v4-flash", Alias: "fast"}}
	models := resolveModels(cfg)
	if len(models) != 1 {
		t.Fatalf("models = %d", len(models))
	}
	if modelClientName(models[0]) != "fast" || models[0].Name != "deepseek-v4-flash" {
		t.Fatalf("model = %#v", models[0])
	}
}
