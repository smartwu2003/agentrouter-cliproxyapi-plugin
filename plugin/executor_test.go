package main

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestBuildUpstreamRequestOpenAINative(t *testing.T) {
	cfg := defaultConfig()
	req := pluginapi.ExecutorRequest{
		Model:        "deepseek-v4-flash",
		SourceFormat: "openai",
		Payload:      []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`),
	}
	endpoint, body, err := buildUpstreamRequest(protocolOpenAI, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "/v1/chat/completions" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if !strings.Contains(string(body), `"stream":false`) {
		t.Fatalf("payload = %s", body)
	}
}

func TestBuildUpstreamRequestClaudeToResponsesUsesCodexWire(t *testing.T) {
	cfg := defaultConfig()
	req := pluginapi.ExecutorRequest{
		Model:        "gpt-6-astra",
		SourceFormat: "claude",
		Payload:      []byte(`{"model":"gpt-6-astra","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`),
	}
	endpoint, body, err := buildUpstreamRequest(protocolResponses, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "/v1/responses" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if !strings.Contains(string(body), `"input"`) {
		t.Fatalf("payload = %s", body)
	}
	if strings.Contains(string(body), `"messages"`) {
		t.Fatalf("payload leaked chat messages: %s", body)
	}
}

func TestBuildUpstreamRequestOpenAIToResponses(t *testing.T) {
	cfg := defaultConfig()
	req := pluginapi.ExecutorRequest{
		Model:        "gpt-6.1-sol",
		SourceFormat: "openai",
		Payload:      []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}
	_, body, err := buildUpstreamRequest(protocolResponses, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"input"`) {
		t.Fatalf("payload = %s", body)
	}
}

func TestWireFormatForResponses(t *testing.T) {
	if got := wireFormatFor(protocolResponses, sdktranslator.FormatOpenAI); got != sdktranslator.FormatCodex {
		t.Fatalf("openai client wire = %v", got)
	}
	if got := wireFormatFor(protocolResponses, sdktranslator.FormatOpenAIResponse); got != sdktranslator.FormatOpenAIResponse {
		t.Fatalf("responses client wire = %v", got)
	}
	if got := wireFormatFor(protocolAnthropic, sdktranslator.FormatCodex); got != sdktranslator.FormatClaude {
		t.Fatalf("claude wire = %v", got)
	}
}

func TestStripSSEDataPrefixBareJSON(t *testing.T) {
	if got := string(stripSSEDataPrefix([]byte("data: {\"a\":1}\n\n"))); got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
	if got := stripSSEDataPrefix([]byte("data: [DONE]\n\n")); got != nil {
		t.Fatalf("DONE should be dropped, got %q", got)
	}
}
