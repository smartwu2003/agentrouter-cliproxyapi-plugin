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

// loginResult is the account payload returned by the New API login endpoint.
type loginResult struct {
	Session dashboardSession
	Token   string
	UserID  string
	Checked bool
	Self    selfUser
	Raw     map[string]any
}

// loginDashboard authenticates against the New API dashboard. AgentRouter
// redeems the daily check-in reward during login, so a successful login is
// also a successful check-in; the response reports `checked_in`.
func loginDashboard(ctx context.Context, cfg pluginConfig) (loginResult, error) {
	if !hasAccountCredentials(cfg) {
		return loginResult{}, errAccountNotAuthorized
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	})
	endpoint := controlEndpoint(cfg, "/api/user/login")
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if errRequest != nil {
		return loginResult{}, errRequest
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return loginResult{}, fmt.Errorf("login: %w", errDo)
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if errRead != nil {
		return loginResult{}, fmt.Errorf("read login response: %w", errRead)
	}
	if response.StatusCode != http.StatusOK {
		return loginResult{}, newUpstreamError(response.StatusCode, summarizeUpstreamError(body))
	}
	var login struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &login); errUnmarshal != nil {
		return loginResult{}, fmt.Errorf("decode login response: %w", errUnmarshal)
	}
	if !login.Success {
		message := strings.TrimSpace(login.Message)
		if message == "" {
			message = "login failed"
		}
		return loginResult{}, fmt.Errorf("%s", message)
	}
	result := loginResult{Raw: login.Data}
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
	if id, okID := login.Data["id"]; okID {
		session.userID = fmt.Sprint(id)
		result.UserID = session.userID
	}
	if checkedIn, okChecked := login.Data["checked_in"]; okChecked {
		if flag, okBool := checkedIn.(bool); okBool && flag {
			session.checkedIn = true
			result.Checked = true
		}
	}
	result.Session = session
	// The login payload is a user profile; reuse it instead of an extra call.
	if raw, errMarshal := json.Marshal(login.Data); errMarshal == nil {
		var user selfUser
		if json.Unmarshal(raw, &user) == nil {
			result.Self = user
		}
	}
	return result, nil
}

// fetchLoginToken reads the durable New API access token with the browser
// session captured by loginDashboard.
func fetchLoginToken(ctx context.Context, cfg pluginConfig, session dashboardSession) (string, error) {
	if session.cookie == "" {
		return "", fmt.Errorf("login returned no session cookie")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodGet, controlEndpoint(cfg, "/api/user/token"), nil)
	if errRequest != nil {
		return "", errRequest
	}
	request.Header.Set("Cookie", session.cookie)
	request.Header.Set("New-Api-User", session.userID)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return "", errDo
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if errRead != nil {
		return "", errRead
	}
	if response.StatusCode != http.StatusOK {
		return "", newUpstreamError(response.StatusCode, summarizeUpstreamError(body))
	}
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return "", errUnmarshal
	}
	if !payload.Success || strings.TrimSpace(payload.Data) == "" {
		return "", fmt.Errorf("account token unavailable: %s", strings.TrimSpace(payload.Message))
	}
	return strings.TrimSpace(payload.Data), nil
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
	result, errLogin := loginDashboard(ctx, cfg)
	if errLogin != nil {
		return dashboardSession{}, errLogin
	}
	storeSession(cfg, result.Session)
	return result.Session, nil
}

// logoutDashboard invalidates the cached dashboard session (best effort).
func logoutDashboard(ctx context.Context, cfg pluginConfig) {
	session, ok := cachedSession(cfg)
	if !ok {
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	endpoint := controlEndpoint(cfg, "/api/user/logout")
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

// flowResult is the outcome of one authorization / sign-in flow.
type flowResult struct {
	Success     bool
	Message     string
	Token       string
	UserID      string
	Username    string
	DisplayName string
	Email       string
	Group       string
	Quota       float64
	Used        float64
	Requests    int64
	CheckedIn   bool
	APIKey      string
	APIKeyID    int64
	APIKeyName  string
	Session     string
}

// authorizeAccount signs into AgentRouter, captures the durable dashboard
// token, and provisions an API key for inference when none is configured.
func authorizeAccount(ctx context.Context, cfg pluginConfig) (flowResult, error) {
	result, errLogin := loginDashboard(ctx, cfg)
	if errLogin != nil {
		return flowResult{Message: errLogin.Error()}, errLogin
	}
	flow := flowResult{
		Token:       result.Token,
		UserID:      result.UserID,
		Username:    result.Self.Username,
		DisplayName: result.Self.DisplayName,
		Email:       result.Self.Email,
		Group:       result.Self.Group,
		Quota:       quotaToUSD(result.Self.Quota),
		Used:        quotaToUSD(result.Self.UsedQuota),
		Requests:    result.Self.RequestCount,
		CheckedIn:   result.Checked,
		Session:     result.Session.cookie,
	}
	if token, errToken := fetchLoginToken(ctx, cfg, result.Session); errToken == nil {
		flow.Token = token
	} else {
		hostLog("warn", "agentrouter dashboard token unavailable; keeping session", map[string]any{"error": errToken.Error()})
	}
	client := accountClient{cfg: cfg, token: flow.Token, userID: result.UserID, session: result.Session.cookie}
	if tokens, errList := client.listTokens(ctx, 100); errList == nil {
		if token, okToken := findUsableToken(tokens, cfg.APIKeyName); okToken {
			flow.APIKey = token.Key
			flow.APIKeyID = token.ID
			flow.APIKeyName = token.Name
		}
	}
	if flow.APIKey == "" && strings.TrimSpace(cfg.APIKey) == "" {
		if created, errCreate := client.createToken(ctx, cfg.APIKeyName); errCreate == nil {
			flow.APIKey = created.Key
			flow.APIKeyID = created.ID
			flow.APIKeyName = created.Name
		} else {
			hostLog("warn", "agentrouter token provisioning failed", map[string]any{"error": errCreate.Error()})
		}
	}
	if flow.APIKey == "" && strings.TrimSpace(cfg.APIKey) != "" {
		flow.APIKey = cfg.APIKey
	}
	flow.Success = true
	if flow.CheckedIn {
		flow.Message = "登录成功，今日签到奖励已到账"
	} else {
		flow.Message = "登录成功"
	}
	return flow, nil
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
// on login, so the canonical flow is logout then login, then reading the
// account balance to report the awarded amount.
func performCheckin(ctx context.Context, cfg pluginConfig) (checkinResult, error) {
	result := checkinResult{Username: cfg.Username}
	beforeQuota := int64(0)
	if session, ok := cachedSession(cfg); ok {
		beforeQuota = sessionQuota(ctx, cfg, session)
	}
	// Invalidating the old session is what makes the next login count as a
	// fresh check-in on AgentRouter.
	logoutDashboard(ctx, cfg)

	login, errLogin := loginDashboard(ctx, cfg)
	if errLogin != nil {
		result.Message = errLogin.Error()
		return result, errLogin
	}
	storeSession(cfg, login.Session)
	result.QuotaAfter = int64(login.Self.Quota)
	if beforeQuota > 0 && result.QuotaAfter > beforeQuota {
		result.QuotaAwarded = result.QuotaAfter - beforeQuota
	}
	result.Success = true
	result.CheckinDate = beijingToday(time.Now())
	result.CheckedInAt = beijingNowText()
	if login.Checked {
		result.Message = "签到成功，奖励已到账"
		if result.QuotaAwarded > 0 {
			result.Message = fmt.Sprintf("签到成功，获得 $%.2f", quotaToUSD(float64(result.QuotaAwarded)))
		}
	} else {
		result.AlreadyDone = true
		result.Message = "登录成功；今天可能已经签到过（AgentRouter 在登录时发放每日奖励）"
	}
	_, _ = updateState(cfg, func(state *pluginState) {
		state.Account = mergeAccount(state.Account, login, cfg)
		state.LastCheckin = &storedCheckin{
			Success:      result.Success,
			Message:      result.Message,
			AlreadyDone:  result.AlreadyDone,
			QuotaAwarded: quotaToUSD(float64(result.QuotaAwarded)),
			QuotaAfter:   quotaToUSD(float64(result.QuotaAfter)),
			CheckinDate:  result.CheckinDate,
			Username:     cfg.Username,
			CheckedInAt:  result.CheckedInAt,
		}
	})
	// Refresh the authoritative status from the reward log, so the card shows the
	// reward even when the login payload omitted checked_in.
	invalidateDailyCheckin()
	resolveDailyCheckin(ctx, cfg, newAccountClient(cfg), true)
	return result, nil
}

// mergeAccount folds a successful login into the persisted account record,
// keeping previously captured credentials when the new response omits them.
func mergeAccount(existing *storedAccount, login loginResult, cfg pluginConfig) *storedAccount {
	account := &storedAccount{}
	if existing != nil {
		*account = *existing
	}
	if login.UserID != "" {
		account.UserID = login.UserID
	}
	if account.AccountToken == "" && strings.TrimSpace(cfg.AccountToken) != "" {
		account.AccountToken = strings.TrimSpace(cfg.AccountToken)
	}
	account.Username = firstNonEmptyString(login.Self.Username, account.Username, cfg.Username)
	account.DisplayName = firstNonEmptyString(login.Self.DisplayName, account.DisplayName)
	account.Email = firstNonEmptyString(login.Self.Email, account.Email)
	account.Group = firstNonEmptyString(login.Self.Group, account.Group)
	if login.Self.Quota != 0 {
		account.Quota = int64(login.Self.Quota)
	}
	if login.Self.UsedQuota != 0 {
		account.UsedQuota = int64(login.Self.UsedQuota)
	}
	if login.Self.RequestCount != 0 {
		account.RequestCount = login.Self.RequestCount
	}
	if login.Session.cookie != "" {
		account.Session = login.Session.cookie
	}
	day := beijingToday(time.Now())
	if login.Checked {
		account.CheckedIn = true
		account.CheckedInAt = beijingNowText()
		account.CheckedInDate = day
	} else if !account.checkedInOn(day) {
		// A new day starts unchecked. Keeping yesterday's flag is what made the
		// card claim a stale "already checked in" for weeks.
		account.CheckedIn = false
		account.CheckedInAt = ""
		account.CheckedInDate = ""
	}
	account.LoginAt = unixNow()
	return account
}

// sessionQuota reads the balance through the dashboard endpoints.
func sessionQuota(ctx context.Context, cfg pluginConfig, session dashboardSession) int64 {
	client := accountClient{cfg: cfg, userID: session.userID, session: session.cookie}
	if user, errUser := client.fetchSelf(ctx); errUser == nil {
		return int64(user.Quota)
	}
	return 0
}

// accountInfo is the shape returned to the dashboard account card.
type accountInfo struct {
	Success        bool           `json:"success"`
	Username       string         `json:"username,omitempty"`
	DisplayName    string         `json:"display_name,omitempty"`
	Email          string         `json:"email,omitempty"`
	Group          string         `json:"group,omitempty"`
	QuotaUSD       float64        `json:"quota_usd"`
	UsedUSD        float64        `json:"used_usd"`
	RequestCount   int64          `json:"request_count"`
	UserID         string         `json:"user_id,omitempty"`
	Models         []string       `json:"models,omitempty"`
	Groups         map[string]any `json:"groups,omitempty"`
	CheckedIn      bool           `json:"checked_in"`
	CheckedInAt    string         `json:"checked_in_at,omitempty"`
	CheckedInDate  string         `json:"checked_in_date,omitempty"`
	CheckedInKnown bool           `json:"checked_in_known"`
	CheckinSource  string         `json:"checkin_source,omitempty"`
	Authorized     bool           `json:"authorized"`
	APIKeyMasked   string         `json:"api_key_masked,omitempty"`
	Message        string         `json:"message,omitempty"`
	LastCheckin    any            `json:"last_checkin,omitempty"`
	// Stale marks values restored from the last successful console read.
	Stale   bool   `json:"stale,omitempty"`
	StaleAt string `json:"stale_at,omitempty"`
	// ConsoleOrigin is the console origin that answered (empty when all failed).
	ConsoleOrigin string `json:"console_origin,omitempty"`
}

// newAccountInfo always exposes the configured inference key. The masked key is
// a configuration fact, not a console read: it must not disappear when the
// console is blocked, which is what made the old card show "-" for 推理密钥.
func newAccountInfo(cfg pluginConfig) accountInfo {
	return accountInfo{
		APIKeyMasked:  maskSecret(cfg.APIKey),
		ConsoleOrigin: consoleOriginMemory.get(),
	}
}

// hydrateAccountFromCache fills the card with the last successful console read
// when the console cannot be reached, so a WAF block no longer blanks every
// field.
func hydrateAccountFromCache(info *accountInfo) {
	state := cachedState()
	if state.Account == nil {
		return
	}
	account := state.Account
	filled := false
	set := func(target *string, value string) {
		if strings.TrimSpace(*target) == "" && strings.TrimSpace(value) != "" {
			*target = value
			filled = true
		}
	}
	set(&info.Username, account.Username)
	set(&info.DisplayName, account.DisplayName)
	set(&info.Email, account.Email)
	set(&info.Group, account.Group)
	set(&info.UserID, account.UserID)
	if info.QuotaUSD == 0 && account.Quota > 0 {
		info.QuotaUSD = quotaToUSD(float64(account.Quota))
		filled = true
	}
	if info.UsedUSD == 0 && account.UsedQuota > 0 {
		info.UsedUSD = quotaToUSD(float64(account.UsedQuota))
		filled = true
	}
	if info.RequestCount == 0 && account.RequestCount > 0 {
		info.RequestCount = account.RequestCount
		filled = true
	}
	if !filled {
		return
	}
	info.Stale = true
	if account.LoginAt > 0 {
		info.StaleAt = time.Unix(account.LoginAt, 0).Format("2006-01-02 15:04")
	} else {
		info.StaleAt = "上次成功读取"
	}
}

// consoleUnavailableMessage explains a console failure without implying the
// inference key is missing.
func consoleUnavailableMessage(info accountInfo, err error) string {
	if err == nil {
		return ""
	}
	if info.Stale {
		return "控制台暂时不可达，已显示 " + info.StaleAt + " 的缓存数据：" + err.Error()
	}
	return "控制台暂时不可达（推理不受影响）：" + err.Error()
}

// fetchAccountInfo reads the account overview through the dashboard API.
func fetchAccountInfo(ctx context.Context, cfg pluginConfig) (accountInfo, error) {
	client := newAccountClient(cfg)
	info := newAccountInfo(cfg)
	if models, errModels := fetchAPIKeyModels(ctx, cfg); errModels == nil {
		for _, model := range models {
			info.Models = append(info.Models, model.ID)
		}
	}
	if state := cachedState(); state.Account != nil {
		info.LastCheckin = state.LastCheckin
	}
	if !client.hasCredentials() && hasAccountCredentials(cfg) {
		// No durable token yet: sign in once to obtain one.
		flow, errAuth := authorizeAccount(ctx, cfg)
		if errAuth != nil {
			info.Message = errAuth.Error()
			hydrateAccountFromCache(&info)
			return info, nil
		}
		applyFlowToState(cfg, flow)
		client = newAccountClient(effectiveConfig())
	}
	if !client.hasCredentials() {
		info.Message = "账号尚未授权：点击「账号授权」登录 AgentRouter，或在配置中填写 account_token 与 user_id"
		applyPersistedDailyCheckin(&info)
		hydrateAccountFromCache(&info)
		return info, nil
	}
	info.Authorized = true
	user, errSelf := client.fetchSelf(ctx)
	if errSelf != nil {
		applyPersistedDailyCheckin(&info)
		hydrateAccountFromCache(&info)
		info.Message = consoleUnavailableMessage(info, errSelf)
		return info, nil
	}
	info.Success = true
	info.Username = user.Username
	info.DisplayName = user.DisplayName
	info.Email = user.Email
	info.Group = user.Group
	info.UserID = fmt.Sprint(user.ID)
	info.QuotaUSD = quotaToUSD(user.Quota)
	info.UsedUSD = quotaToUSD(user.UsedQuota)
	info.RequestCount = user.RequestCount
	info.ConsoleOrigin = consoleOriginMemory.get()
	// The profile payload has no check-in flag. Derive today's status from the
	// reward log so a check-in performed on the website is reflected here.
	applyDailyCheckin(&info, resolveDailyCheckin(ctx, cfg, client, false))
	if client.token == "" {
		info.Message = "当前只有浏览器会话凭据，部分接口（令牌管理）可能不可用；建议重新执行账号授权以获取访问令牌"
	}
	if groups, errGroups := client.fetchGroups(ctx); errGroups == nil {
		info.Groups = groups
	}
	return info, nil
}

// applyFlowToState persists the credentials captured by authorizeAccount.
func applyFlowToState(cfg pluginConfig, flow flowResult) {
	_, _ = updateState(cfg, func(state *pluginState) {
		account := &storedAccount{}
		if state.Account != nil {
			*account = *state.Account
		}
		if strings.TrimSpace(flow.Token) != "" {
			account.AccountToken = strings.TrimSpace(flow.Token)
			state.AccountToken = strings.TrimSpace(flow.Token)
		}
		if strings.TrimSpace(flow.UserID) != "" {
			account.UserID = strings.TrimSpace(flow.UserID)
			state.UserID = strings.TrimSpace(flow.UserID)
		}
		if strings.TrimSpace(flow.Session) != "" {
			account.Session = strings.TrimSpace(flow.Session)
		}
		if strings.TrimSpace(flow.Username) != "" {
			account.Username = strings.TrimSpace(flow.Username)
			state.Username = strings.TrimSpace(flow.Username)
		}
		account.DisplayName = firstNonEmptyString(flow.DisplayName, account.DisplayName)
		account.Email = firstNonEmptyString(flow.Email, account.Email)
		account.Group = firstNonEmptyString(flow.Group, account.Group)
		account.Quota = int64(flow.Quota * quotaPerUnit)
		account.UsedQuota = int64(flow.Used * quotaPerUnit)
		account.RequestCount = flow.Requests
		if strings.TrimSpace(flow.APIKey) != "" {
			account.APIKey = strings.TrimSpace(flow.APIKey)
			account.APIKeyID = flow.APIKeyID
			account.APIKeyName = flow.APIKeyName
			if strings.TrimSpace(cfg.APIKey) == "" {
				state.APIKey = strings.TrimSpace(flow.APIKey)
			}
		}
		account.LoginAt = unixNow()
		day := beijingToday(time.Now())
		if flow.CheckedIn {
			account.CheckedIn = true
			account.CheckedInAt = beijingNowText()
			account.CheckedInDate = day
		} else if !account.checkedInOn(day) {
			account.CheckedIn = false
			account.CheckedInAt = ""
			account.CheckedInDate = ""
		}
		state.Account = account
		if state.APIKeyName == "" {
			state.APIKeyName = cfg.APIKeyName
		}
	})
}

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
		if _, errCheckin := performCheckin(context.Background(), effectiveConfig()); errCheckin != nil {
			hostLog("warn", "agentrouter automatic check-in failed", map[string]any{"error": errCheckin.Error()})
		} else {
			hostLog("info", "agentrouter automatic check-in completed", nil)
		}
	}
}
