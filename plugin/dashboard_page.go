package main

import (
	"html"
	"strings"
)

// renderDashboard builds the self-contained browser resource page served at
// /v0/resource/plugins/agentrouter/status. The GUI injects the management key
// into localStorage (cli-proxy-auth) before the page loads, so the page can
// call the plugin's authenticated management routes.
func renderDashboard(cfg pluginConfig) string {
	replacer := strings.NewReplacer(
		"{{BASE_URL}}", html.EscapeString(cfg.BaseURL),
		"{{PLUGIN_VERSION}}", html.EscapeString(pluginVersion),
		"{{DATA_DIR}}", html.EscapeString(statePath(cfg)),
	)
	return replacer.Replace(dashboardTemplate)
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
