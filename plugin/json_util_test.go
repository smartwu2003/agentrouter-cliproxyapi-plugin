package main

import (
	"strings"
	"testing"
)

func TestSetJSONStringPreservesRest(t *testing.T) {
	in := []byte(`{"model":"a","x":1, "y":"z"}`)
	out := string(setJSONString(in, "model", "b"))
	if !strings.Contains(out, `"model":"b"`) || !strings.Contains(out, `"x":1`) {
		t.Fatalf("out = %s", out)
	}
}

func TestEnsureResponseModel(t *testing.T) {
	in := []byte(`{"model":"upstream","choices":[]}`)
	out := string(ensureResponseModel(in, "alias"))
	if !strings.Contains(out, `"model":"alias"`) {
		t.Fatalf("out = %s", out)
	}
}

func TestHasJSONType(t *testing.T) {
	if hasJSONType([]byte(`{"status":"completed"}`)) {
		t.Fatal("plain object should not have type")
	}
	if !hasJSONType([]byte(`{"type":"response.completed"}`)) {
		t.Fatal("wrapped object should have type")
	}
}

func TestNormalizeChatCompletionsBodyStream(t *testing.T) {
	out := string(normalizeChatCompletionsBody([]byte(`{"stream":true}`), true))
	if !strings.Contains(out, `"include_usage":true`) {
		t.Fatalf("out = %s", out)
	}
}
