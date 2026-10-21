package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// setJSONString sets a top-level string field, preserving the rest of the
// payload byte-for-byte when possible.
func setJSONString(body []byte, key, value string) []byte {
	if key == "" {
		return body
	}
	encoded, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return body
	}
	fieldPattern := []byte(`"` + key + `":`)
	index := bytes.Index(body, fieldPattern)
	if index >= 0 {
		start := index + len(fieldPattern)
		rest := bytes.TrimLeft(body[start:], " \t\r\n")
		if len(rest) > 0 && rest[0] == '"' {
			end := findJSONStringEnd(rest)
			if end >= 0 {
				var out bytes.Buffer
				out.Write(body[:start])
				out.Write(encoded)
				out.Write(rest[end+1:])
				return out.Bytes()
			}
		}
	}
	var obj map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(body, &obj); errUnmarshal != nil {
		return body
	}
	obj[key] = encoded
	out, errMarshalObj := json.Marshal(obj)
	if errMarshalObj != nil {
		return body
	}
	return out
}

// findJSONStringEnd returns the index of the closing quote of a JSON string
// starting at position 0, or -1 when the string is not terminated.
func findJSONStringEnd(raw []byte) int {
	escaped := false
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		if escaped {
			escaped = false
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '"':
			return i
		}
	}
	return -1
}

// ensureResponseModel rewrites the top-level model field of a response body to
// the client-requested name so clients never see AgentRouter's upstream ID.
func ensureResponseModel(body []byte, model string) []byte {
	if model == "" || len(body) == 0 {
		return body
	}
	var obj map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(body, &obj); errUnmarshal != nil {
		return body
	}
	if _, ok := obj["model"]; !ok {
		return body
	}
	encoded, _ := json.Marshal(model)
	if bytes.Equal(bytes.TrimSpace(obj["model"]), encoded) {
		return body
	}
	obj["model"] = encoded
	out, errMarshal := json.Marshal(obj)
	if errMarshal != nil {
		return body
	}
	return out
}

// normalizeChatCompletionsBody applies AgentRouter-friendly defaults to a Chat
// Completions payload.
func normalizeChatCompletionsBody(body []byte, stream bool) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	if stream {
		obj["stream"] = json.RawMessage("true")
		var streamOptions map[string]json.RawMessage
		if raw, ok := obj["stream_options"]; ok && json.Unmarshal(raw, &streamOptions) == nil {
			streamOptions["include_usage"] = json.RawMessage("true")
			encoded, err := json.Marshal(streamOptions)
			if err == nil {
				obj["stream_options"] = encoded
			}
		} else {
			obj["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		}
	} else {
		obj["stream"] = json.RawMessage("false")
	}
	out, errMarshal := json.Marshal(obj)
	if errMarshal != nil {
		return body
	}
	return out
}

// normalizeResponsesBody applies AgentRouter-friendly defaults to an OpenAI
// Responses payload.
func normalizeResponsesBody(body []byte, stream bool) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	if stream {
		obj["stream"] = json.RawMessage("true")
	} else {
		obj["stream"] = json.RawMessage("false")
	}
	// store=true requires an upstream storage backend AgentRouter does not
	// offer; force stateless handling.
	if _, ok := obj["store"]; !ok {
		obj["store"] = json.RawMessage("false")
	}
	out, errMarshal := json.Marshal(obj)
	if errMarshal != nil {
		return body
	}
	return out
}

// normalizeMessagesBody applies AgentRouter-friendly defaults to an Anthropic
// Messages payload.
func normalizeMessagesBody(body []byte, stream bool) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return body
	}
	if stream {
		obj["stream"] = json.RawMessage("true")
	} else {
		delete(obj, "stream")
	}
	out, errMarshal := json.Marshal(obj)
	if errMarshal != nil {
		return body
	}
	return out
}

// upstreamModelFromPayload extracts the model field from a request payload.
func upstreamModelFromPayload(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
}

// hasJSONType reports whether the payload already carries a top-level "type"
// field (Codex/Responses terminal events do, plain Responses objects do not).
func hasJSONType(body []byte) bool {
	var payload struct {
		Type string `json:"type"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return false
	}
	return strings.TrimSpace(payload.Type) != ""
}
