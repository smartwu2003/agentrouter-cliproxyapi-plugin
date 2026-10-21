package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dashboardSession holds the browser-session cookie and the numeric user ID
// required by AgentRouter's New API dashboard endpoints.
type dashboardSession struct {
	cookie    string
	userID    string
	username  string
	checkedIn bool
	loginAt   time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]dashboardSession
}

var dashboardSessions = &sessionStore{sessions: map[string]dashboardSession{}}

func sessionKey(cfg pluginConfig) string {
	return strings.ToLower(cfg.BaseURL) + "|" + strings.ToLower(cfg.Username)
}

func cachedSession(cfg pluginConfig) (dashboardSession, bool) {
	dashboardSessions.mu.Lock()
	defer dashboardSessions.mu.Unlock()
	session, ok := dashboardSessions.sessions[sessionKey(cfg)]
	if !ok || session.cookie == "" {
		return dashboardSession{}, false
	}
	return session, true
}

func storeSession(cfg pluginConfig, session dashboardSession) {
	dashboardSessions.mu.Lock()
	dashboardSessions.sessions[sessionKey(cfg)] = session
	dashboardSessions.mu.Unlock()
}

func dropSession(cfg pluginConfig) {
	dashboardSessions.mu.Lock()
	delete(dashboardSessions.sessions, sessionKey(cfg))
	dashboardSessions.mu.Unlock()
}

func hasAccountCredentials(cfg pluginConfig) bool {
	return strings.TrimSpace(cfg.Username) != "" && strings.TrimSpace(cfg.Password) != ""
}

// loginDashboard authenticates against the New API dashboard. AgentRouter
// redeems the daily check-in reward during login, so a successful login is
// also a successful check-in; the response reports `checked_in`.
func loginDashboard(ctx context.Context, cfg pluginConfig) (dashboardSession, error) {
	if !hasAccountCredentials(cfg) {
		return dashboardSession{}, fmt.Errorf("account credentials are not configured (username/password)")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	})
	endpoint := strings.TrimSuffix(cfg.BaseURL, "/") + "/api/user/login"
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if errRequest != nil {
		return dashboardSession{}, errRequest
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return dashboardSession{}, fmt.Errorf("login: %w", errDo)
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if errRead != nil {
		return dashboardSession{}, fmt.Errorf("read login response: %w", errRead)
	}
	if response.StatusCode != http.StatusOK {
		return dashboardSession{}, newUpstreamError(response.StatusCode, summarizeUpstreamError(body))
	}
	var login struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &login); errUnmarshal != nil {
		return dashboardSession{}, fmt.Errorf("decode login response: %w", errUnmarshal)
	}
	if !login.Success {
		message := strings.TrimSpace(login.Message)
		if message == "" {
			message = "login failed"
		}
		return dashboardSession{}, fmt.Errorf("%s", message)
	}
	session := dashboardSession{username: cfg.Username, loginAt: time.Now()}
	if cookies := response.Header.Values("Set-Cookie"); len(cookies) > 0 {
		parts := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			if pair, okPair := cookiePair(cookie); okPair {
				parts = append(parts, pair)
			}
		}
		session.cookie = strings.Join(parts, "; ")
	}
	if session.cookie == "" {
		return dashboardSession{}, fmt.Errorf("login succeeded but no session cookie was returned")
	}
	if id, okID := login.Data["id"]; okID {
		session.userID = fmt.Sprint(id)
	}
	if checkedIn, okChecked := login.Data["checked_in"]; okChecked {
		if flag, okBool := checkedIn.(bool); okBool && flag {
			session.checkedIn = true
		}
	}
	return session, nil
}

func cookiePair(setCookie string) (string, bool) {
	pair := strings.TrimSpace(strings.SplitN(setCookie, ";", 2)[0])
	if pair == "" || !strings.Contains(pair, "=") {
		return "", false
	}
	return pair, true
}

// ensureSession returns a cached dashboard session, logging in when needed.
func ensureSession(ctx context.Context, cfg pluginConfig) (dashboardSession, error) {
	if session, ok := cachedSession(cfg); ok {
		return session, nil
	}
	session, errLogin := loginDashboard(ctx, cfg)
	if errLogin != nil {
		return dashboardSession{}, errLogin
	}
	storeSession(cfg, session)
	return session, nil
}

// logoutDashboard invalidates the cached dashboard session (best effort).
func logoutDashboard(ctx context.Context, cfg pluginConfig) {
	session, ok := cachedSession(cfg)
	if !ok {
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	endpoint := strings.TrimSuffix(cfg.BaseURL, "/") + "/api/user/logout"
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if errRequest == nil {
		request.Header.Set("Cookie", session.cookie)
		request.Header.Set("New-Api-User", session.userID)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", cfg.UserAgent)
		response, errDo := controlPlaneClient(cfg).Do(request)
		if errDo == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
		}
	}
	dropSession(cfg)
}

// checkinResult describes one check-in attempt for the management UI.
type checkinResult struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	AlreadyDone  bool   `json:"already_done"`
	QuotaAwarded int64  `json:"quota_awarded"`
	QuotaAfter   int64  `json:"quota_after"`
	CheckinDate  string `json:"checkin_date"`
	Username     string `json:"username"`
	CheckedInAt  string `json:"checked_in_at"`
}

// performCheckin runs the daily check-in. AgentRouter redeems the daily reward
// on login, so the canonical flow is logout → login, then reading the account
// balance to report the awarded amount.
func performCheckin(ctx context.Context, cfg pluginConfig) (checkinResult, error) {
	if !hasAccountCredentials(cfg) {
		return checkinResult{}, fmt.Errorf("account credentials are not configured (username/password)")
	}
	result := checkinResult{Username: cfg.Username}
	beforeQuota := int64(0)
	if session, ok := cachedSession(cfg); ok {
		beforeQuota, _ = fetchQuota(ctx, cfg, session)
	}
	// Invalidating the old session is what makes the next login count as a
	// fresh check-in on AgentRouter.
	logoutDashboard(ctx, cfg)

	session, errLogin := loginDashboard(ctx, cfg)
	if errLogin != nil {
		result.Message = errLogin.Error()
		return result, errLogin
	}
	storeSession(cfg, session)
	if afterQuota, errQuota := fetchQuota(ctx, cfg, session); errQuota == nil {
		result.QuotaAfter = afterQuota
		if beforeQuota > 0 && afterQuota > beforeQuota {
			result.QuotaAwarded = afterQuota - beforeQuota
		}
	}
	result.Success = true
	result.CheckinDate = time.Now().Format("2006-01-02")
	result.CheckedInAt = time.Now().Format("2006-01-02 15:04:05")
	if session.checkedIn {
		result.Message = "签到成功，奖励已到账"
		if result.QuotaAwarded > 0 {
			result.Message = fmt.Sprintf("签到成功，获得 $%.2f", quotaToUSD(float64(result.QuotaAwarded)))
		}
	} else {
		result.AlreadyDone = true
		result.Message = "登录成功；今天可能已经签到过（AgentRouter 在登录时发放每日奖励）"
	}
	return result, nil
}

// fetchQuota reads the account balance from the dashboard API.
func fetchQuota(ctx context.Context, cfg pluginConfig, session dashboardSession) (int64, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoint := strings.TrimSuffix(cfg.BaseURL, "/") + "/api/user/self"
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if errRequest != nil {
		return 0, errRequest
	}
	request.Header.Set("Cookie", session.cookie)
	request.Header.Set("New-Api-User", session.userID)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return 0, errDo
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if errRead != nil {
		return 0, errRead
	}
	if response.StatusCode != http.StatusOK {
		return 0, newUpstreamError(response.StatusCode, summarizeUpstreamError(body))
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Quota int64 `json:"quota"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return 0, errUnmarshal
	}
	return payload.Data.Quota, nil
}

// accountInfo is the shape returned to the dashboard account card.
type accountInfo struct {
	Username       string   `json:"username,omitempty"`
	DisplayName    string   `json:"display_name,omitempty"`
	Email          string   `json:"email,omitempty"`
	Group          string   `json:"group,omitempty"`
	QuotaUSD       float64  `json:"quota_usd"`
	UsedUSD        float64  `json:"used_usd"`
	RequestCount   int64    `json:"request_count"`
	Models         []string `json:"models,omitempty"`
	CheckedInToday *bool    `json:"checked_in_today,omitempty"`
	Message        string   `json:"message,omitempty"`
}

// fetchAccountInfo reads the account overview. Full account details need the
// dashboard session; with only an API key the plugin returns the model list.
func fetchAccountInfo(ctx context.Context, cfg pluginConfig) (accountInfo, error) {
	info := accountInfo{}
	if models, errModels := fetchAPIKeyModels(ctx, cfg); errModels == nil {
		for _, model := range models {
			info.Models = append(info.Models, model.ID)
		}
	}
	if !hasAccountCredentials(cfg) {
		info.Message = "配置 username/password 后可查看余额、分组与签到状态"
		return info, nil
	}
	session, errLogin := ensureSession(ctx, cfg)
	if errLogin != nil {
		return info, errLogin
	}
	details, errDetails := fetchDashboardAccount(ctx, cfg, session)
	if errDetails != nil {
		return info, errDetails
	}
	details.Models = info.Models
	return details, nil
}

// fetchDashboardAccount reads /api/user/self for a logged-in dashboard session.
func fetchDashboardAccount(ctx context.Context, cfg pluginConfig, session dashboardSession) (accountInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimSuffix(cfg.BaseURL, "/")+"/api/user/self", nil)
	if errRequest != nil {
		return accountInfo{}, errRequest
	}
	request.Header.Set("Cookie", session.cookie)
	request.Header.Set("New-Api-User", session.userID)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return accountInfo{}, errDo
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if errRead != nil {
		return accountInfo{}, errRead
	}
	if response.StatusCode != http.StatusOK {
		return accountInfo{}, newUpstreamError(response.StatusCode, summarizeUpstreamError(body))
	}
	var payload struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return accountInfo{}, errUnmarshal
	}
	if !payload.Success {
		return accountInfo{}, fmt.Errorf("%s", strings.TrimSpace(payload.Message))
	}
	info := accountInfo{}
	info.Username, _ = payload.Data["username"].(string)
	info.DisplayName, _ = payload.Data["display_name"].(string)
	info.Email, _ = payload.Data["email"].(string)
	info.Group, _ = payload.Data["group"].(string)
	info.QuotaUSD = quotaToUSD(payload.Data["quota"])
	info.UsedUSD = quotaToUSD(payload.Data["used_quota"])
	if count, okCount := payload.Data["request_count"].(float64); okCount {
		info.RequestCount = int64(count)
	}
	checkedIn := session.checkedIn
	info.CheckedInToday = &checkedIn
	return info, nil
}

func quotaToUSD(value any) float64 {
	number, ok := value.(float64)
	if !ok {
		return 0
	}
	return number / quotaPerUnit
}

const quotaPerUnit = 500000.0

// checkinSchedulerState triggers the configured automatic check-in after the
// Beijing-time hour passes. It restarts whenever the plugin config reloads.
type checkinSchedulerState struct {
	mu         sync.Mutex
	generation uint64
}

var checkinState = &checkinSchedulerState{}

func (s *checkinSchedulerState) configure(cfg pluginConfig) {
	s.mu.Lock()
	s.generation++
	generation := s.generation
	s.mu.Unlock()
	if !cfg.CheckinOnStartup || !hasAccountCredentials(cfg) {
		return
	}
	go s.loop(generation, cfg)
}

func (s *checkinSchedulerState) loop(generation uint64, cfg pluginConfig) {
	loc, errLoc := time.LoadLocation("Asia/Shanghai")
	if errLoc != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	hour := cfg.CheckinHour
	if hour < 0 || hour > 23 {
		hour = 9
	}
	for {
		s.mu.Lock()
		current := s.generation
		s.mu.Unlock()
		if current != generation {
			return
		}
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, 5, 0, 0, loc)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		for {
			s.mu.Lock()
			current = s.generation
			s.mu.Unlock()
			if current != generation {
				timer.Stop()
				return
			}
			remaining := time.Until(next)
			if remaining <= 0 {
				break
			}
			var wait time.Duration
			if remaining > time.Minute {
				wait = time.Minute
			} else {
				wait = remaining
			}
			time.Sleep(wait)
		}
		timer.Stop()
		if _, errCheckin := performCheckin(context.Background(), cfg); errCheckin != nil {
			hostLog("warn", "agentrouter automatic check-in failed", map[string]any{"error": errCheckin.Error()})
		} else {
			hostLog("info", "agentrouter automatic check-in completed", nil)
		}
	}
}
