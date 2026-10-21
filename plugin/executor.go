package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	_ "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator/builtin"
)

type rpcExecutorRequest struct {
	pluginapi.ExecutorRequest
	StreamID       string `json:"stream_id,omitempty"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// executeRequest handles a non-streaming model execution.
func executeRequest(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode executor request: %w", errUnmarshal)
	}
	cfg := loadedConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("agentrouter api_key is not configured")
	}
	resp, errRun := runUpstream(context.Background(), cfg, req.ExecutorRequest, false)
	if errRun != nil {
		return nil, errRun
	}
	defer resp.Body.Close()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, fmt.Errorf("read agentrouter response: %w", errRead)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newUpstreamError(resp.StatusCode, summarizeUpstreamError(body))
	}
	out := translateNonStream(req.ExecutorRequest, body)
	return okEnvelope(pluginapi.ExecutorResponse{
		Payload: out,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
	})
}

// executeStreamRequest handles streaming execution through the host stream bridge.
func executeStreamRequest(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode executor stream request: %w", errUnmarshal)
	}
	cfg := loadedConfig()
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("agentrouter api_key is not configured")
	}
	resp, errRun := runUpstream(context.Background(), cfg, req.ExecutorRequest, true)
	if errRun != nil {
		return nil, errRun
	}
	headers := http.Header{"Content-Type": []string{"text/event-stream"}, "Cache-Control": []string{"no-cache"}}
	if req.StreamID == "" {
		// Hosts without an async stream bridge: collect every translated chunk
		// synchronously and return them in the response envelope.
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			return nil, newUpstreamError(resp.StatusCode, summarizeUpstreamError(body))
		}
		channels := make(chan pluginapi.ExecutorStreamChunk, 64)
		if errStream := pumpStream(req.ExecutorRequest, resp.Body, func(chunk []byte) error {
			if len(chunk) == 0 {
				return nil
			}
			channels <- pluginapi.ExecutorStreamChunk{Payload: bytes.Clone(chunk)}
			return nil
		}); errStream != nil {
			close(channels)
			return nil, errStream
		}
		close(channels)
		var chunks []pluginapi.ExecutorStreamChunk
		for chunk := range channels {
			chunks = append(chunks, chunk)
		}
		return okEnvelope(struct {
			Headers http.Header                     `json:"headers,omitempty"`
			Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
		}{Headers: headers, Chunks: chunks})
	}
	go relayStream(req, resp)
	return okEnvelope(struct {
		Headers http.Header `json:"headers,omitempty"`
	}{Headers: headers})
}

// relayStream reads the upstream stream, translates each frame and forwards it
// to the host stream bridge, then closes the bridge.
func relayStream(req rpcExecutorRequest, resp *streamedResponse) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		hostStreamClose(req.StreamID, summarizeUpstreamError(body))
		return
	}
	errStream := pumpStream(req.ExecutorRequest, resp.Body, func(chunk []byte) error {
		if len(chunk) == 0 {
			return nil
		}
		return hostStreamEmit(req.StreamID, chunk)
	})
	if errStream != nil {
		hostStreamClose(req.StreamID, errStream.Error())
		return
	}
	hostStreamClose(req.StreamID, "")
}

type streamedResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// runUpstream translates and sends one request to AgentRouter.
func runUpstream(ctx context.Context, cfg pluginConfig, req pluginapi.ExecutorRequest, stream bool) (*streamedResponse, error) {
	protocol := protocolForRequest(cfg, req.Model, req.SourceFormat)
	endpoint, payload, errBuild := buildUpstreamRequest(protocol, cfg, req)
	if errBuild != nil {
		return nil, errBuild
	}
	url := strings.TrimSuffix(cfg.BaseURL, "/") + endpoint
	httpReq, errNew := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if errNew != nil {
		return nil, fmt.Errorf("build agentrouter request: %w", errNew)
	}
	applyUpstreamHeaders(httpReq, cfg, protocol, req, stream)
	client := upstreamClient{host: req.HTTPClient}
	var resp *http.Response
	var errDo error
	if stream {
		resp, errDo = client.doStream(httpReq)
	} else {
		resp, errDo = client.do(httpReq)
	}
	if errDo != nil {
		return nil, fmt.Errorf("agentrouter request failed: %w", errDo)
	}
	return &streamedResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}

// upstreamClient adapts the host HTTP client (so request logs capture the
// traffic) or a plain client when the host did not provide one.
type upstreamClient struct {
	host pluginapi.HostHTTPClient
}

func (c upstreamClient) do(req *http.Request) (*http.Response, error) {
	if c.host == nil {
		return fallbackHTTPClient.Do(req)
	}
	body, errRead := readRequestBody(req)
	if errRead != nil {
		return nil, errRead
	}
	resp, errDo := c.host.Do(req.Context(), pluginapi.HTTPRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header.Clone(),
		Body:    body,
	})
	if errDo != nil {
		return nil, errDo
	}
	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Headers,
		Body:       io.NopCloser(bytes.NewReader(resp.Body)),
		Request:    req,
	}, nil
}

func (c upstreamClient) doStream(req *http.Request) (*http.Response, error) {
	if c.host == nil {
		return fallbackHTTPClient.Do(req)
	}
	body, errRead := readRequestBody(req)
	if errRead != nil {
		return nil, errRead
	}
	resp, errDo := c.host.DoStream(req.Context(), pluginapi.HTTPRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header.Clone(),
		Body:    body,
	})
	if errDo != nil {
		return nil, errDo
	}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for chunk := range resp.Chunks {
			if chunk.Err != nil {
				_ = pw.CloseWithError(chunk.Err)
				return
			}
			if len(chunk.Payload) == 0 {
				continue
			}
			if _, errWrite := pw.Write(chunk.Payload); errWrite != nil {
				return
			}
		}
	}()
	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Headers,
		Body:       pr,
		Request:    req,
	}, nil
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

var fallbackHTTPClient = &http.Client{Transport: http.DefaultTransport}

// buildUpstreamRequest converts the canonical request into the wire format
// AgentRouter expects for the chosen protocol.
func buildUpstreamRequest(protocol string, cfg pluginConfig, req pluginapi.ExecutorRequest) (string, []byte, error) {
	body := bytes.Clone(req.Payload)
	source := sdktranslator.FromString(req.SourceFormat)
	target := wireFormatFor(protocol, source)
	upstreamModel := upstreamModelName(cfg, req.Model)
	if target != "" && source != target {
		converted, errTranslate, ok := translateRequest(source, target, req)
		if errTranslate != nil {
			return "", nil, errTranslate
		}
		if ok {
			body = converted
		}
	}
	body = setJSONString(body, "model", upstreamModel)
	switch protocol {
	case protocolOpenAI:
		body = normalizeChatCompletionsBody(body, req.Stream)
		return "/v1/chat/completions", body, nil
	case protocolResponses:
		body = normalizeResponsesBody(body, req.Stream)
		return "/v1/responses", body, nil
	case protocolAnthropic:
		body = normalizeMessagesBody(body, req.Stream)
		return "/v1/messages", body, nil
	default:
		return "", nil, fmt.Errorf("unsupported agentrouter protocol %q", protocol)
	}
}

// wireFormatFor picks the built-in translator format that matches the upstream
// protocol. The Responses API is represented as Codex when the client speaks a
// different protocol, because Codex responses have native converters to Claude
// and OpenAI Chat, while Responses-to-Claude/OpenAI response conversion is not
// registered in the built-in translator table.
func wireFormatFor(protocol string, source sdktranslator.Format) sdktranslator.Format {
	switch protocol {
	case protocolOpenAI:
		return sdktranslator.FormatOpenAI
	case protocolAnthropic:
		return sdktranslator.FormatClaude
	case protocolResponses:
		if source == sdktranslator.FormatOpenAIResponse || source == sdktranslator.FormatCodex {
			return source
		}
		return sdktranslator.FormatCodex
	default:
		return ""
	}
}

// upstreamModelName maps a client-visible alias back to the upstream model ID.
func upstreamModelName(cfg pluginConfig, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return requested
	}
	for _, spec := range cfg.Models {
		if modelClientName(spec) == requested {
			return spec.Name
		}
	}
	return requested
}

// translateRequest uses the SDK's built-in translators. ok=false means no
// translation was available and the caller must keep the original payload.
func translateRequest(from, to sdktranslator.Format, req pluginapi.ExecutorRequest) ([]byte, error, bool) {
	envelope := sdktranslator.TranslateRequestEnvelope(context.Background(), from, to, sdktranslator.RequestEnvelope{
		Format: from,
		Model:  req.Model,
		Stream: req.Stream,
		Body:   req.Payload,
	})
	if envelope.Err != nil {
		return nil, envelope.Err, false
	}
	if bytes.Equal(envelope.Body, req.Payload) {
		return envelope.Body, nil, false
	}
	return envelope.Body, nil, true
}

// translateNonStream converts an upstream response body into the response
// format requested by the host.
func translateNonStream(req pluginapi.ExecutorRequest, body []byte) []byte {
	protocol := protocolForRequest(loadedConfig(), req.Model, req.SourceFormat)
	source := sdktranslator.FromString(req.SourceFormat)
	if source == "" {
		source = sdktranslator.FromString(outputNameToFormat(protocol))
	}
	upstream := wireFormatFor(protocol, source)
	if upstream == "" || source == "" || upstream == source {
		return ensureResponseModel(body, req.Model)
	}
	from := upstream
	to := source
	originalPayload := req.OriginalRequest
	if len(originalPayload) == 0 {
		originalPayload = req.Payload
	}
	// Codex/Responses non-stream converters expect a terminal response.completed
	// envelope; AgentRouter returns a plain Responses object, so wrap it.
	if from == sdktranslator.FormatCodex || from == sdktranslator.FormatOpenAIResponse {
		if !hasJSONType(body) {
			wrapped := []byte(`{"type":"response.completed","response":`)
			wrapped = append(wrapped, bytes.TrimSpace(body)...)
			wrapped = append(wrapped, '}')
			body = wrapped
		}
	}
	var param any
	out := sdktranslator.TranslateNonStream(context.Background(), from, to, req.Model, originalPayload, req.Payload, body, &param)
	if len(out) == 0 {
		return ensureResponseModel(body, req.Model)
	}
	return ensureResponseModel(out, req.Model)
}

func outputNameToFormat(name string) string {
	switch name {
	case protocolAnthropic:
		return "claude"
	case protocolResponses:
		return "openai-response"
	case protocolOpenAI:
		return "openai"
	default:
		return ""
	}
}

// pumpStream parses an upstream SSE stream and emits frames translated to the
// host's expected response format.
func pumpStream(req pluginapi.ExecutorRequest, body io.Reader, emit func([]byte) error) error {
	protocol := protocolForRequest(loadedConfig(), req.Model, req.SourceFormat)
	source := sdktranslator.FromString(req.SourceFormat)
	if source == "" {
		source = sdktranslator.FromString(outputNameToFormat(protocol))
	}
	upstream := wireFormatFor(protocol, source)
	needsTranslation := upstream != "" && source != "" && upstream != source
	from, to := upstream, source
	openAITarget := source == sdktranslator.FormatOpenAI
	originalPayload := req.OriginalRequest
	if len(originalPayload) == 0 {
		originalPayload = req.Payload
	}
	var param any
	// emitFrame delivers one logical frame in the shape the host expects:
	// bare JSON for OpenAI targets, complete SSE events for Claude/Responses.
	emitFrame := func(frame []byte) error {
		if openAITarget {
			payload := stripSSEDataPrefix(frame)
			if len(payload) == 0 {
				return nil
			}
			return emit(payload)
		}
		out := bytes.TrimRight(frame, "\n")
		if len(out) == 0 {
			return nil
		}
		return emit(append(append([]byte(nil), out...), byte(10), byte(10)))
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var frame bytes.Buffer
	flush := func() error {
		if frame.Len() == 0 {
			return nil
		}
		raw := bytes.Clone(frame.Bytes())
		frame.Reset()
		if !needsTranslation {
			if len(extractSSEData(raw)) == 0 {
				return nil
			}
			if bytes.Equal(extractSSEData(raw), []byte("[DONE]")) {
				return nil
			}
			return emitFrame(raw)
		}
		data := extractSSEData(raw)
		if len(data) == 0 {
			return nil
		}
		// The [DONE] sentinel is forwarded to the translator: several response
		// translators emit their terminal event (e.g. response.completed for
		// the Responses target) only after seeing it.
		frames := sdktranslator.TranslateStream(context.Background(), from, to, req.Model, originalPayload, req.Payload, append([]byte("data: "), data...), &param)
		for _, translated := range frames {
			if len(translated) == 0 {
				continue
			}
			if errEmit := emitFrame(translated); errEmit != nil {
				return errEmit
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			if errFlush := flush(); errFlush != nil {
				return errFlush
			}
			continue
		}
		if frame.Len() > 0 {
			frame.WriteByte(byte(10))
		}
		frame.Write(line)
	}
	if errFlush := flush(); errFlush != nil {
		return errFlush
	}
	if errScan := scanner.Err(); errScan != nil {
		return fmt.Errorf("read agentrouter stream: %w", errScan)
	}
	return nil
}

// extractSSEData gathers the data payload of one SSE frame.
func extractSSEData(frame []byte) []byte {
	var lines [][]byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			lines = append(lines, bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return bytes.Join(lines, []byte("\n"))
}

// applyUpstreamHeaders sets authentication, the whitelisted User-Agent and
// protocol-specific headers on the upstream request.
func applyUpstreamHeaders(req *http.Request, cfg pluginConfig, protocol string, executorReq pluginapi.ExecutorRequest, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	switch protocol {
	case protocolAnthropic:
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		if stream {
			req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		}
	case protocolResponses:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		req.Header.Set("OpenAI-Beta", "responses=experimental")
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if strings.HasPrefix(strings.ToLower(userAgent), "claude-cli") {
		req.Header.Set("X-App", "cli")
		req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	}
	copyClientHeaders(req, executorReq.Headers)
}

func copyClientHeaders(dst *http.Request, src http.Header) {
	if len(src) == 0 {
		return
	}
	pass := map[string]bool{
		"X-Claude-Code-Session-Id":    true,
		"X-Stainless-Lang":            true,
		"X-Stainless-Package-Version": true,
		"X-Stainless-Runtime":         true,
		"X-Stainless-Runtime-Version": true,
		"X-Stainless-Os":              true,
		"X-Stainless-Arch":            true,
	}
	for name, values := range src {
		if !pass[http.CanonicalHeaderKey(name)] || len(values) == 0 {
			continue
		}
		dst.Header.Set(name, values[0])
	}
}

// stripSSEDataPrefix turns one translated SSE frame into the bare JSON payload
// the host's OpenAI stream writer expects.
func stripSSEDataPrefix(frame []byte) []byte {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 {
		return nil
	}
	data := extractSSEData(trimmed)
	if len(data) > 0 && bytes.Equal(data, []byte("[DONE]")) {
		return nil
	}
	if len(data) > 0 {
		return data
	}
	return trimmed
}
