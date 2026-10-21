package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestRenderDashboardReplacesPlaceholders guards the three-part template split:
// the markup references {{STYLE}}, {{SCRIPT}}, {{BASE_URL}} and
// {{PLUGIN_VERSION}}, and a missing replacer would ship literal braces to the
// browser.
func TestRenderDashboardReplacesPlaceholders(t *testing.T) {
	cfg := defaultConfig()
	page := renderDashboard(cfg)
	if strings.Contains(page, "{{") {
		t.Fatalf("rendered page still contains template placeholders")
	}
	if !strings.Contains(page, dashboardCSS[:80]) {
		t.Fatal("stylesheet was not inlined")
	}
	if !strings.Contains(page, "const API = {") {
		t.Fatal("client script was not inlined")
	}
	if !strings.Contains(page, cfg.BaseURL) {
		t.Fatal("base URL was not substituted")
	}
}

// TestDashboardScriptOnlyUsesRenderedIDs catches drift between the console
// script and the markup: every element the script looks up must exist.
func TestDashboardScriptOnlyUsesRenderedIDs(t *testing.T) {
	page := renderDashboard(defaultConfig())
	ids := map[string]bool{}
	for _, match := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		ids[match[1]] = true
	}
	lookups := regexp.MustCompile(`\$\(\s*'([A-Za-z0-9_-]+)'\s*\)`).FindAllStringSubmatch(dashboardJS, -1)
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, match := range lookups {
		id := match[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if !ids[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("script references missing element ids: %v", missing)
	}
}

// TestDashboardTemplateHasNoBackticks keeps the Go raw string valid.
func TestDashboardAssetsHaveNoBackticks(t *testing.T) {
	for name, value := range map[string]string{
		"dashboardCSS": dashboardCSS,
		"dashboardJS":  dashboardJS,
	} {
		if strings.Contains(value, "`") {
			t.Fatalf("%s contains a backtick", name)
		}
	}
}
