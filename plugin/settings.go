package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// storedAccount is the AgentRouter dashboard account captured by a login.
// The session cookie is short-lived; the access token is the long-lived New
// API credential used for dashboard calls after login.
type storedAccount struct {
	Username     string `json:"username,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	Email        string `json:"email,omitempty"`
	Group        string `json:"group,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	AccountToken string `json:"account_token,omitempty"`
	Session      string `json:"session,omitempty"`
	Quota        int64  `json:"quota,omitempty"`
	UsedQuota    int64  `json:"used_quota,omitempty"`
	RequestCount int64  `json:"request_count,omitempty"`
	CheckedIn    bool   `json:"checked_in,omitempty"`
	CheckedInAt  string `json:"checked_in_at,omitempty"`
	LoginAt      int64  `json:"login_at,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	APIKeyID     int64  `json:"api_key_id,omitempty"`
	APIKeyName   string `json:"api_key_name,omitempty"`
}

// storedCheckin is the last check-in attempt reported to the dashboard.
type storedCheckin struct {
	Success      bool    `json:"success"`
	Message      string  `json:"message"`
	AlreadyDone  bool    `json:"already_done"`
	QuotaAwarded float64 `json:"quota_awarded"`
	QuotaAfter   float64 `json:"quota_after"`
	CheckinDate  string  `json:"checkin_date"`
	Username     string  `json:"username"`
	CheckedInAt  string  `json:"checked_in_at"`
}

// pluginState is the dashboard-owned persisted state. It is stored next to the
// plugin (or in data_dir) so explicit user changes survive restarts without
// rewriting config.yaml.
type pluginState struct {
	APIKey           string         `json:"api_key,omitempty"`
	UserAgent        string         `json:"user_agent,omitempty"`
	BaseURL          string         `json:"base_url,omitempty"`
	DashboardBaseURL string         `json:"dashboard_base_url,omitempty"`
	ProxyURL         string         `json:"proxy_url,omitempty"`
	AccountToken     string         `json:"account_token,omitempty"`
	UserID           string         `json:"user_id,omitempty"`
	Username         string         `json:"username,omitempty"`
	Password         string         `json:"password,omitempty"`
	RememberPassword bool           `json:"remember_password,omitempty"`
	APIKeyName       string         `json:"api_key_name,omitempty"`
	Account          *storedAccount `json:"account,omitempty"`
	LastCheckin      *storedCheckin `json:"last_checkin,omitempty"`
}

type stateStore struct {
	mu   sync.Mutex
	path string
	data pluginState
}

var pluginStore = &stateStore{}

func statePath(cfg pluginConfig) string {
	if dir := strings.TrimSpace(cfg.DataDir); dir != "" {
		return filepath.Join(dir, "agentrouter-state.json")
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, "plugins", pluginName+"-state.json")
	}
	return filepath.Join(os.TempDir(), pluginName+"-state.json")
}

// loadState reads the persisted dashboard state. Missing or corrupt files
// yield an empty state so the plugin always starts.
func loadState(cfg pluginConfig) pluginState {
	path := statePath(cfg)
	pluginStore.mu.Lock()
	defer pluginStore.mu.Unlock()
	pluginStore.path = path
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		pluginStore.data = pluginState{}
		return pluginState{}
	}
	var state pluginState
	if errUnmarshal := json.Unmarshal(raw, &state); errUnmarshal != nil {
		pluginStore.data = pluginState{}
		return pluginState{}
	}
	pluginStore.data = state
	return state
}

// saveState persists the dashboard state atomically with owner-only
// permissions. It contains credentials, so it is never logged.
func saveState(cfg pluginConfig, state pluginState) error {
	pluginStore.mu.Lock()
	defer pluginStore.mu.Unlock()
	path := pluginStore.path
	if path == "" {
		path = statePath(cfg)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(path), 0o700); errMkdir != nil {
		return errMkdir
	}
	body, errMarshal := json.MarshalIndent(state, "", "  ")
	if errMarshal != nil {
		return errMarshal
	}
	temp, errTemp := os.CreateTemp(filepath.Dir(path), ".agentrouter-state-*")
	if errTemp != nil {
		return errTemp
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if errChmod := temp.Chmod(0o600); errChmod != nil {
		temp.Close()
		return errChmod
	}
	if _, errWrite := temp.Write(body); errWrite != nil {
		temp.Close()
		return errWrite
	}
	if errSync := temp.Sync(); errSync != nil {
		temp.Close()
		return errSync
	}
	if errClose := temp.Close(); errClose != nil {
		return errClose
	}
	if errRename := os.Rename(tempName, path); errRename != nil {
		return errRename
	}
	pluginStore.data = state
	pluginStore.path = path
	return nil
}

func cachedState() pluginState {
	pluginStore.mu.Lock()
	defer pluginStore.mu.Unlock()
	return pluginStore.data
}

// updateState mutates the cached state under the store lock and persists it.
func updateState(cfg pluginConfig, mutate func(*pluginState)) (pluginState, error) {
	current := cachedState()
	mutate(&current)
	if errSave := saveState(cfg, current); errSave != nil {
		return current, errSave
	}
	return current, nil
}

// applyState overlays dashboard values on top of the YAML configuration.
func applyState(cfg pluginConfig, state pluginState) pluginConfig {
	if strings.TrimSpace(state.APIKey) != "" {
		cfg.APIKey = strings.TrimSpace(state.APIKey)
	}
	if strings.TrimSpace(state.UserAgent) != "" {
		cfg.UserAgent = strings.TrimSpace(state.UserAgent)
	}
	if strings.TrimSpace(state.BaseURL) != "" {
		cfg.BaseURL = normalizeBaseURL(state.BaseURL)
	}
	if strings.TrimSpace(state.DashboardBaseURL) != "" {
		cfg.DashboardBaseURL = normalizeBaseURL(state.DashboardBaseURL)
	}
	if strings.TrimSpace(state.ProxyURL) != "" {
		cfg.ProxyURL = strings.TrimSpace(state.ProxyURL)
	}
	if state.Account != nil {
		if strings.TrimSpace(state.Account.AccountToken) != "" {
			cfg.AccountToken = strings.TrimSpace(state.Account.AccountToken)
		}
		if strings.TrimSpace(state.Account.UserID) != "" {
			cfg.UserID = strings.TrimSpace(state.Account.UserID)
		}
		if strings.TrimSpace(state.Account.Username) != "" {
			cfg.Username = strings.TrimSpace(state.Account.Username)
		}
		if strings.TrimSpace(state.Account.APIKey) != "" {
			cfg.APIKey = strings.TrimSpace(state.Account.APIKey)
		}
	}
	if strings.TrimSpace(state.Username) != "" {
		cfg.Username = strings.TrimSpace(state.Username)
	}
	if strings.TrimSpace(state.Password) != "" {
		cfg.Password = strings.TrimSpace(state.Password)
	}
	if strings.TrimSpace(state.AccountToken) != "" {
		cfg.AccountToken = strings.TrimSpace(state.AccountToken)
	}
	if strings.TrimSpace(state.UserID) != "" {
		cfg.UserID = strings.TrimSpace(state.UserID)
	}
	if strings.TrimSpace(state.APIKeyName) != "" {
		cfg.APIKeyName = strings.TrimSpace(state.APIKeyName)
	}
	return cfg
}

// effectiveConfig merges config.yaml with persisted dashboard overrides.
func effectiveConfig() pluginConfig {
	return applyState(loadedConfig(), cachedState())
}

// accountReady reports whether dashboard account calls can be attempted.
func accountReady(cfg pluginConfig) bool {
	if strings.TrimSpace(cfg.AccountToken) != "" || strings.TrimSpace(cfg.UserID) != "" {
		return true
	}
	if state := cachedState(); state.Account != nil {
		return strings.TrimSpace(state.Account.AccountToken) != "" ||
			strings.TrimSpace(state.Account.Session) != ""
	}
	return hasAccountCredentials(cfg)
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", 8) + value[len(value)-4:]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func unixNow() int64 { return time.Now().Unix() }

// recentLogEntry is one row of the AgentRouter usage log.
type recentLogEntry struct {
	ID           int64   `json:"id"`
	CreatedAt    int64   `json:"created_at"`
	Type         int     `json:"type"`
	Content      string  `json:"content"`
	ModelName    string  `json:"model_name"`
	TokenName    string  `json:"token_name"`
	Quota        int64   `json:"quota"`
	PromptTokens int64   `json:"prompt_tokens"`
	CompleteTok  int64   `json:"completion_tokens"`
	UseTime      float64 `json:"use_time"`
	Stream       bool    `json:"is_stream"`
	Group        string  `json:"group"`
}

// logPage is the paginated /api/log/self response.
type logPage struct {
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
	Items    []recentLogEntry `json:"items"`
}

// dashboardPoint is one /api/data/self aggregation bucket.
type dashboardPoint struct {
	ModelName string `json:"model_name"`
	CreatedAt int64  `json:"created_at"`
	TokenUsed int64  `json:"token_used"`
	Count     int64  `json:"count"`
	Quota     int64  `json:"quota"`
}

// logTypeLabel maps New API log types to display labels.
func logTypeLabel(logType int) string {
	switch logType {
	case 0:
		return "全部"
	case 1:
		return "充值"
	case 2:
		return "消费"
	case 4:
		return "签到"
	case 5:
		return "错误"
	default:
		return "其他"
	}
}

// quotaPerUnit is AgentRouter's New API quota unit (500000 units = $1).
const quotaPerUnit = 500000.0
