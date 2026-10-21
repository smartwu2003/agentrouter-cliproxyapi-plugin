package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// controlPlaneClient returns an HTTP client for AgentRouter management calls
// (model catalogue, account details, check-in). Inference never uses it: that
// traffic goes through the host HTTP client so request logs and proxy policy
// stay consistent.
func controlPlaneClient(cfg pluginConfig) *http.Client {
	proxyURL := strings.TrimSpace(cfg.ProxyURL)
	key := "direct"
	if proxyURL != "" {
		key = proxyURL
	}
	controlPlaneClients.mu.Lock()
	defer controlPlaneClients.mu.Unlock()
	if client, ok := controlPlaneClients.byKey[key]; ok {
		return client
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if proxyURL != "" {
		if parsed, errParse := url.Parse(proxyURL); errParse == nil && parsed.Host != "" {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}
	controlPlaneClients.byKey[key] = client
	return client
}

var controlPlaneClients = struct {
	mu    sync.Mutex
	byKey map[string]*http.Client
}{byKey: map[string]*http.Client{}}
