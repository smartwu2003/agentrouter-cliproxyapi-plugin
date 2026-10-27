package main

import (
	"net/http"
	"time"
)

// controlPlaneClient returns the client for the configured console origin.
// Call sites that already know the origin use controlPlaneClientFor so the
// proxy decision follows the origin (proxied origins use proxy_url, the
// mainland origin always connects directly).
func controlPlaneClient(cfg pluginConfig) *http.Client {
	return controlPlaneClientFor(cfg, accountBaseURL(cfg))
}

func controlPlaneClientFor(cfg pluginConfig, origin string) *http.Client {
	return originHTTPClient(cfg, origin, 60*time.Second)
}
