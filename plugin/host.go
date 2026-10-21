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
	"fmt"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// hostStreamEmit forwards one translated SSE frame to the host stream bridge.
func hostStreamEmit(streamID string, payload []byte) error {
	if streamID == "" {
		return fmt.Errorf("stream id is missing")
	}
	_, errCall := callHost(pluginabi.MethodHostStreamEmit, map[string]any{
		"stream_id": streamID,
		"payload":   payload,
	})
	return errCall
}

// hostStreamClose ends a host stream, optionally reporting an error.
func hostStreamClose(streamID, errorMessage string) {
	if streamID == "" {
		return
	}
	payload := map[string]any{"stream_id": streamID}
	if errorMessage != "" {
		payload["error"] = errorMessage
	}
	_, _ = callHost(pluginabi.MethodHostStreamClose, payload)
}

// hostLog writes a message through the host logger so plugin diagnostics land
// in the same log stream as the server.
func hostLog(level, message string, fields map[string]any) {
	payload := map[string]any{"level": level, "message": message}
	if len(fields) > 0 {
		payload["fields"] = fields
	}
	_, _ = callHost(pluginabi.MethodHostLog, payload)
}

// hostAuthList returns the host credential records.
func hostAuthList() ([]pluginapi.HostAuthFileEntry, error) {
	raw, errCall := callHost(pluginabi.MethodHostAuthList, map[string]any{})
	if errCall != nil {
		return nil, errCall
	}
	var response struct {
		Files []pluginapi.HostAuthFileEntry `json:"files"`
	}
	if errUnmarshal := json.Unmarshal(raw, &response); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host auth list: %w", errUnmarshal)
	}
	return response.Files, nil
}

// callHost invokes one host callback and decodes the standard JSON envelope.
func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host callback %s: %w", method, errMarshal)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return nil, fmt.Errorf("allocate host callback %s", method)
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}
	callCode := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(callCode))
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host envelope %s: %w", method, errUnmarshal)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, int(callCode))
	}
	return append(json.RawMessage(nil), env.Result...), nil
}

// storeHostAPI records the host function table for later callbacks.
func storeHostAPI(host *C.cliproxy_host_api) {
	C.store_host_api(host)
}
