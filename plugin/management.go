package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	resourceDashboardPath = "/status"
	routeRefreshModels    = "/agentrouter/refresh-models"
	routeAccount          = "/agentrouter/account"
	routeCheckin          = "/agentrouter/checkin"
	routeTest             = "/agentrouter/test"
	routeModels           = "/agentrouter/models"
	routeSettings         = "/agentrouter/settings"
	routeLogin            = "/agentrouter/login"
	routeToken            = "/agentrouter/login-token"
	routeLogs             = "/agentrouter/logs"
	routeDashboard        = "/agentrouter/dashboard"
	routeCheckinStatus    = "/agentrouter/checkin-status"
	routeAPIKeys          = "/agentrouter/api-keys"
	routeAnnouncements    = "/agentrouter/announcements"
)

func managementRegistration() managementRegistrationResponse {
	return managementRegistrationResponse{
		Resources: []pluginapi.ResourceRoute{
			{
				Path:        resourceDashboardPath,
				Menu:        "AgentRouter",
				Description: "AgentRouter 账号、数据看板、模型、使用日志与签到。",
			},
		},
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodPost, Path: routeRefreshModels},
			{Method: http.MethodGet, Path: routeAccount},
			{Method: http.MethodPost, Path: routeCheckin},
			{Method: http.MethodPost, Path: routeTest},
			{Method: http.MethodGet, Path: routeModels},
			{Method: http.MethodGet, Path: routeSettings},
			{Method: http.MethodPost, Path: routeSettings},
			{Method: http.MethodPost, Path: routeLogin},
			{Method: http.MethodPost, Path: routeToken},
			{Method: http.MethodGet, Path: routeLogs},
			{Method: http.MethodGet, Path: routeDashboard},
			{Method: http.MethodGet, Path: routeCheckinStatus},
			{Method: http.MethodGet, Path: routeAPIKeys},
			{Method: http.MethodGet, Path: routeAnnouncements},
		},
	}
}

// handleManagement dispatches plugin management and resource requests.
func handleManagement(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
	}
	path := normalizeManagementPath(req.Path)
	switch path {
	case resourceDashboardPath:
		return okEnvelope(pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       []byte(renderDashboard(effectiveConfig())),
		})
	case routeModels:
		return okEnvelope(modelsResponse())
	case routeSettings:
		if strings.EqualFold(req.Method, http.MethodPost) || strings.EqualFold(req.Method, http.MethodPut) {
			return okEnvelope(saveSettingsResponse(req))
		}
		return okEnvelope(settingsResponse())
	case routeRefreshModels:
		return okEnvelope(refreshModelsResponse(req))
	case routeAccount:
		return okEnvelope(accountResponse(req))
	case routeCheckin:
		return okEnvelope(checkinResponse(req))
	case routeCheckinStatus:
		return okEnvelope(checkinStatusResponse(req))
	case routeTest:
		return okEnvelope(testResponse(req))
	case routeLogin:
		return okEnvelope(loginResponse(req))
	case routeToken:
		return okEnvelope(loginTokenResponse(req))
	case routeLogs:
		return okEnvelope(logsResponse(req))
	case routeDashboard:
		return okEnvelope(dashboardResponse(req))
	case routeAPIKeys:
		return okEnvelope(apiKeysResponse(req))
	case routeAnnouncements:
		return okEnvelope(announcementsResponse())
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]any{"success": false, "message": "route not found"}))
	}
}

func normalizeManagementPath(path string) string {
	path = strings.TrimSpace(path)
	if index := strings.Index(path, "/v0/resource/plugins/"+pluginName); index >= 0 {
		path = path[index+len("/v0/resource/plugins/"+pluginName):]
	} else if index := strings.Index(path, "/v0/management"); index >= 0 {
		path = path[index+len("/v0/management"):]
	}
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/")
	if path == "" {
		return resourceDashboardPath
	}
	return path
}

func jsonResponse(status int, payload any) pluginapi.ManagementResponse {
	body, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		body = []byte(`{"success":false,"message":"encode error"}`)
		status = http.StatusInternalServerError
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       body,
	}
}

func modelsResponse() pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	models := resolveModels(cfg)
	out := make([]map[string]any, 0, len(models))
	for _, spec := range models {
		out = append(out, map[string]any{
			"name":                  spec.Name,
			"client_name":           modelClientName(spec),
			"display_name":          firstNonEmptyString(spec.DisplayName, spec.Name),
			"protocol":              protocolForModel(cfg, spec.Name),
			"protocols":             spec.Protocols,
			"context_length":        spec.ContextLength,
			"max_completion_tokens": spec.MaxCompletionTokens,
			"input_modalities":      inputModalitiesFor(spec),
		})
	}
	_, lastSync, lastErr := catalogueSnapshot()
	return jsonResponse(http.StatusOK, map[string]any{
		"success":    true,
		"models":     out,
		"last_sync":  formatTime(lastSync),
		"last_error": lastErr,
		"base_url":   cfg.BaseURL,
	})
}

func settingsResponse() pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	state := cachedState()
	return jsonResponse(http.StatusOK, map[string]any{
		"success":              true,
		"base_url":             cfg.BaseURL,
		"user_agent":           cfg.UserAgent,
		"proxy_url":            cfg.ProxyURL,
		"dashboard_base_url":   cfg.DashboardBaseURL,
		"auto_discover_models": autoDiscoverModels(cfg),
		"has_api_key":          strings.TrimSpace(cfg.APIKey) != "",
		"api_key_masked":       maskSecret(cfg.APIKey),
		"has_account":          accountReady(cfg),
		"has_account_token":    strings.TrimSpace(cfg.AccountToken) != "",
		"username":             cfg.Username,
		"user_id":              cfg.UserID,
		"display_name":         stateAccountName(state),
		"api_key_name":         cfg.APIKeyName,
		"data_dir":             statePath(cfg),
		"checkin_on_startup":   cfg.CheckinOnStartup,
		"checkin_hour":         cfg.CheckinHour,
		"last_checkin":         state.LastCheckin,
		"version":              pluginVersion,
	})
}

func stateAccountName(state pluginState) string {
	if state.Account == nil {
		return ""
	}
	return firstNonEmptyString(state.Account.DisplayName, state.Account.Username)
}

// saveSettingsResponse persists dashboard configuration changes.
func saveSettingsResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	var payload struct {
		APIKey           *string `json:"api_key"`
		ClearAPIKey      bool    `json:"clear_api_key"`
		UserAgent        *string `json:"user_agent"`
		BaseURL          *string `json:"base_url"`
		ProxyURL         *string `json:"proxy_url"`
		DashboardBaseURL *string `json:"dashboard_base_url"`
		AccountToken     *string `json:"account_token"`
		UserID           *string `json:"user_id"`
		Username         *string `json:"username"`
		Password         *string `json:"password"`
	}
	if errUnmarshal := json.Unmarshal(req.Body, &payload); errUnmarshal != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON body"})
	}
	cfg := loadedConfig()
	state, errSave := updateState(cfg, func(state *pluginState) {
		if payload.APIKey != nil {
			state.APIKey = strings.TrimSpace(*payload.APIKey)
		}
		if payload.ClearAPIKey {
			state.APIKey = ""
		}
		if payload.UserAgent != nil {
			state.UserAgent = strings.TrimSpace(*payload.UserAgent)
		}
		if payload.BaseURL != nil {
			state.BaseURL = normalizeBaseURL(*payload.BaseURL)
		}
		if payload.ProxyURL != nil {
			state.ProxyURL = strings.TrimSpace(*payload.ProxyURL)
		}
		if payload.DashboardBaseURL != nil {
			state.DashboardBaseURL = normalizeBaseURL(*payload.DashboardBaseURL)
		}
		if payload.AccountToken != nil {
			state.AccountToken = strings.TrimSpace(*payload.AccountToken)
			if state.Account == nil {
				state.Account = &storedAccount{}
			}
			state.Account.AccountToken = state.AccountToken
		}
		if payload.UserID != nil {
			state.UserID = strings.TrimSpace(*payload.UserID)
			if state.Account == nil {
				state.Account = &storedAccount{}
			}
			state.Account.UserID = state.UserID
		}
		if payload.Username != nil {
			state.Username = strings.TrimSpace(*payload.Username)
		}
		if payload.Password != nil {
			state.Password = *payload.Password
		}
	})
	if errSave != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]any{"success": false, "message": "保存失败：" + errSave.Error()})
	}
	// Refresh the in-memory configuration so the change applies immediately.
	merged := applyState(cfg, state)
	activeConfigMu.Lock()
	activeConfig = merged
	activeConfigMu.Unlock()
	primeCatalogue(merged)
	return jsonResponse(http.StatusOK, map[string]any{
		"success":      true,
		"message":      "配置已保存",
		"has_api_key":  strings.TrimSpace(merged.APIKey) != "",
		"has_account":  accountReady(merged),
		"user_agent":   merged.UserAgent,
		"base_url":     merged.BaseURL,
		"api_key_name": merged.APIKeyName,
	})
}

func refreshModelsResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	if strings.TrimSpace(cfg.APIKey) == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"success": false, "message": "尚未配置 API Key，请先执行账号授权或手动填写"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	models, errRefresh := refreshCatalogue(ctx, cfg)
	if errRefresh != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errRefresh.Error()})
	}
	names := make([]string, 0, len(models))
	for _, item := range models {
		names = append(names, item.ID)
	}
	return jsonResponse(http.StatusOK, map[string]any{
		"success": true,
		"models":  names,
		"count":   len(names),
	})
}

func accountResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	info, errInfo := fetchAccountInfo(context.Background(), cfg)
	if errInfo != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errInfo.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]any{"success": true, "account": info})
}

func checkinResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	result, errCheckin := performCheckin(context.Background(), cfg)
	if errCheckin != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{
			"success": false,
			"message": errCheckin.Error(),
			"result":  result,
		})
	}
	return jsonResponse(http.StatusOK, map[string]any{"success": true, "result": result})
}

func checkinStatusResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	client := newAccountClient(cfg)
	state := cachedState()
	payload := map[string]any{
		"success":       true,
		"last_checkin":  state.LastCheckin,
		"has_account":   client.hasCredentials(),
		"checked_in":    state.Account != nil && state.Account.CheckedIn,
		"checked_in_at": "",
	}
	if state.Account != nil {
		payload["checked_in_at"] = state.Account.CheckedInAt
	}
	month := strings.TrimSpace(req.Query.Get("month"))
	if client.hasCredentials() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if data, errStatus := client.fetchCheckinStatus(ctx, month); errStatus == nil {
			payload["server"] = data
		} else if !strings.Contains(errStatus.Error(), "Invalid URL") {
			// AgentRouter redeems the daily reward inside the login flow and
			// does not expose the newer New API check-in endpoint.
			payload["server_error"] = errStatus.Error()
		}
	}
	return jsonResponse(http.StatusOK, payload)
}

func testResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	model := ""
	if len(req.Body) > 0 {
		var payload struct {
			Model string `json:"model"`
		}
		if errUnmarshal := json.Unmarshal(req.Body, &payload); errUnmarshal == nil {
			model = strings.TrimSpace(payload.Model)
		}
	}
	if model == "" {
		if models := resolveModels(cfg); len(models) > 0 {
			model = models[0].Name
		}
	}
	if model == "" {
		model = "deepseek-v4-flash"
	}
	protocol := protocolForModel(cfg, model)
	started := time.Now()
	responseBody, status, errProbe := probeModel(context.Background(), cfg, model, protocol)
	latency := time.Since(started)
	if errProbe != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{
			"success":    false,
			"model":      model,
			"protocol":   protocol,
			"status":     status,
			"latency_ms": latency.Milliseconds(),
			"message":    errProbe.Error(),
		})
	}
	snippet := string(responseBody)
	if len(snippet) > 1500 {
		snippet = snippet[:1500]
	}
	return jsonResponse(http.StatusOK, map[string]any{
		"success":    true,
		"model":      model,
		"protocol":   protocol,
		"status":     status,
		"latency_ms": latency.Milliseconds(),
		"response":   snippet,
	})
}

// loginResponse runs the account authorization from the dashboard.
func loginResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	if !hasAccountCredentials(cfg) {
		return jsonResponse(http.StatusBadRequest, map[string]any{"success": false, "message": "请填写账号与密码"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	flow, errAuth := authorizeAccount(ctx, cfg)
	if errAuth != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errAuth.Error()})
	}
	applyFlowToState(cfg, flow)
	state, errSave := updateState(cfg, func(state *pluginState) {
		state.Username = firstNonEmptyString(flow.Username, state.Username, cfg.Username)
		state.Password = cfg.Password
	})
	if errSave != nil {
		hostLog("warn", "agentrouter state save failed", map[string]any{"error": errSave.Error()})
	}
	merged := applyState(loadedConfig(), state)
	activeConfigMu.Lock()
	activeConfig = merged
	activeConfigMu.Unlock()
	primeCatalogue(merged)
	return jsonResponse(http.StatusOK, map[string]any{
		"success":      true,
		"message":      flow.Message,
		"account":      accountSnapshot(flow),
		"has_api_key":  strings.TrimSpace(merged.APIKey) != "",
		"api_key_name": flow.APIKeyName,
		"checked_in":   flow.CheckedIn,
	})
}

// loginTokenResponse stores a system token pasted from the console.
func loginTokenResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(effectiveConfig(), req.Body)
	var payload struct {
		Token    string `json:"token"`
		UserID   string `json:"user_id"`
		Username string `json:"username"`
		Session  string `json:"session"`
	}
	if errUnmarshal := json.Unmarshal(req.Body, &payload); errUnmarshal != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON body"})
	}
	if strings.TrimSpace(payload.Token) == "" && strings.TrimSpace(payload.Session) != "" {
		saved, errSession := saveSessionCookie(cfg, payload.Session, payload.UserID)
		if errSession != nil {
			return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errSession.Error()})
		}
		merged := applyState(loadedConfig(), saved)
		activeConfigMu.Lock()
		activeConfig = merged
		activeConfigMu.Unlock()
		primeCatalogue(merged)
		return jsonResponse(http.StatusOK, map[string]any{"success": true, "message": "浏览器会话已保存", "user_id": saved.UserID})
	}
	state, errSave := saveManualToken(cfg, payload.Token, payload.UserID, payload.Username)
	if errSave != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errSave.Error()})
	}
	merged := applyState(loadedConfig(), state)
	activeConfigMu.Lock()
	activeConfig = merged
	activeConfigMu.Unlock()
	primeCatalogue(merged)
	return jsonResponse(http.StatusOK, map[string]any{
		"success": true,
		"message": "访问令牌已保存",
		"user_id": state.UserID,
	})
}

func accountSnapshot(flow flowResult) map[string]any {
	return map[string]any{
		"username":      flow.Username,
		"display_name":  flow.DisplayName,
		"email":         flow.Email,
		"group":         flow.Group,
		"quota_usd":     flow.Quota,
		"used_usd":      flow.Used,
		"request_count": flow.Requests,
		"checked_in":    flow.CheckedIn,
		"api_key_name":  flow.APIKeyName,
	}
}

// logsResponse returns the AgentRouter usage log page.
func logsResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	client := newAccountClient(cfg)
	if !client.hasCredentials() {
		return jsonResponse(http.StatusOK, map[string]any{"success": false, "authorized": false, "message": "账号尚未授权"})
	}
	query := req.Query
	logType := -1
	if value := strings.TrimSpace(query.Get("type")); value != "" {
		if parsed, errAtoi := strconv.Atoi(value); errAtoi == nil {
			logType = parsed
		}
	}
	page := 0
	if value := strings.TrimSpace(query.Get("page")); value != "" {
		if parsed, errAtoi := strconv.Atoi(value); errAtoi == nil && parsed >= 0 {
			page = parsed
		}
	}
	pageSize := 20
	if value := strings.TrimSpace(query.Get("page_size")); value != "" {
		if parsed, errAtoi := strconv.Atoi(value); errAtoi == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	var start, end int64
	if value := strings.TrimSpace(query.Get("start")); value != "" {
		if parsed, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
			start = parsed
		}
	}
	if value := strings.TrimSpace(query.Get("end")); value != "" {
		if parsed, errParse := strconv.ParseInt(value, 10, 64); errParse == nil {
			end = parsed
		}
	}
	model := strings.TrimSpace(query.Get("model"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pageData, errLogs := client.fetchLogs(ctx, logType, page, pageSize, model, start, end)
	if errLogs != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errLogs.Error()})
	}
	items := make([]map[string]any, 0, len(pageData.Items))
	for _, item := range pageData.Items {
		items = append(items, map[string]any{
			"id":                item.ID,
			"created_at":        item.CreatedAt,
			"created_at_text":   time.Unix(item.CreatedAt, 0).In(beijingLocation()).Format("2006-01-02 15:04:05"),
			"type":              item.Type,
			"type_label":        logTypeLabel(item.Type),
			"content":           item.Content,
			"model_name":        item.ModelName,
			"token_name":        item.TokenName,
			"quota_usd":         quotaToUSD(float64(item.Quota)),
			"prompt_tokens":     item.PromptTokens,
			"completion_tokens": item.CompleteTok,
			"use_time":          item.UseTime,
			"stream":            item.Stream,
			"group":             item.Group,
		})
	}
	return jsonResponse(http.StatusOK, map[string]any{
		"success":   true,
		"page":      pageData.Page,
		"page_size": pageData.PageSize,
		"total":     pageData.Total,
		"items":     items,
	})
}

// dashboardResponse returns the usage aggregation used for charts.
func dashboardResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	client := newAccountClient(cfg)
	if !client.hasCredentials() {
		return jsonResponse(http.StatusOK, map[string]any{"success": false, "authorized": false, "message": "账号尚未授权"})
	}
	days := 7
	if value := strings.TrimSpace(req.Query.Get("days")); value != "" {
		if parsed, errAtoi := strconv.Atoi(value); errAtoi == nil && parsed > 0 && parsed <= 90 {
			days = parsed
		}
	}
	defaultTime := strings.TrimSpace(req.Query.Get("interval"))
	if defaultTime == "" {
		if days <= 2 {
			defaultTime = "hour"
		} else {
			defaultTime = "day"
		}
	}
	end := time.Now().Unix()
	start := end - int64(days)*86400
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	points, errData := client.fetchDashboard(ctx, start, end, defaultTime)
	if errData != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errData.Error()})
	}
	stat, _ := client.fetchLogStat(ctx, start, end, "")
	byModel := map[string]map[string]any{}
	timeline := map[string]map[string]any{}
	var totalQuota, totalTokens, totalCount int64
	for _, point := range points {
		bucket := byModel[point.ModelName]
		if bucket == nil {
			bucket = map[string]any{"model": point.ModelName, "quota_usd": 0.0, "tokens": int64(0), "count": int64(0)}
			byModel[point.ModelName] = bucket
		}
		bucket["quota_usd"] = bucket["quota_usd"].(float64) + quotaToUSD(float64(point.Quota))
		bucket["tokens"] = bucket["tokens"].(int64) + point.TokenUsed
		bucket["count"] = bucket["count"].(int64) + point.Count
		key := time.Unix(point.CreatedAt, 0).In(beijingLocation()).Format("01-02 15:04")
		slot := timeline[key]
		if slot == nil {
			slot = map[string]any{"time": key, "quota_usd": 0.0, "tokens": int64(0), "count": int64(0)}
			timeline[key] = slot
		}
		slot["quota_usd"] = slot["quota_usd"].(float64) + quotaToUSD(float64(point.Quota))
		slot["tokens"] = slot["tokens"].(int64) + point.TokenUsed
		slot["count"] = slot["count"].(int64) + point.Count
		totalQuota += point.Quota
		totalTokens += point.TokenUsed
		totalCount += point.Count
	}
	modelList := make([]map[string]any, 0, len(byModel))
	for _, item := range byModel {
		modelList = append(modelList, item)
	}
	keys := make([]string, 0, len(timeline))
	for key := range timeline {
		keys = append(keys, key)
	}
	sortStrings(keys)
	timelineList := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		timelineList = append(timelineList, timeline[key])
	}
	payload := map[string]any{
		"success":      true,
		"days":         days,
		"interval":     defaultTime,
		"points":       points,
		"by_model":     modelList,
		"timeline":     timelineList,
		"total_usd":    quotaToUSD(float64(totalQuota)),
		"total_tokens": totalTokens,
		"total_count":  totalCount,
		"stat":         stat,
	}
	if user, errSelf := client.fetchSelf(ctx); errSelf == nil {
		payload["quota_usd"] = quotaToUSD(user.Quota)
		payload["used_usd"] = quotaToUSD(user.UsedQuota)
		payload["request_count"] = user.RequestCount
		payload["group"] = user.Group
		payload["username"] = user.Username
	}
	return jsonResponse(http.StatusOK, payload)
}

func apiKeysResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	client := newAccountClient(cfg)
	if !client.hasCredentials() {
		return jsonResponse(http.StatusOK, map[string]any{"success": false, "authorized": false, "message": "账号尚未授权"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	tokens, errList := client.listTokens(ctx, 100)
	if errList != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errList.Error()})
	}
	items := make([]map[string]any, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, map[string]any{
			"id":              token.ID,
			"name":            token.Name,
			"key_masked":      maskSecret(token.Key),
			"status":          token.Status,
			"created_time":    token.CreatedTime,
			"accessed_time":   token.AccessedTime,
			"expired_time":    token.ExpiredTime,
			"unlimited_quota": token.UnlimitedQuota,
			"used_usd":        quotaToUSD(float64(token.UsedQuota)),
			"in_use":          strings.TrimSpace(token.Key) != "" && token.Key == cfg.APIKey,
		})
	}
	return jsonResponse(http.StatusOK, map[string]any{
		"success":      true,
		"items":        items,
		"api_key_name": cfg.APIKeyName,
	})
}

func announcementsResponse() pluginapi.ManagementResponse {
	cfg := effectiveConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, errFetch := fetchAnnouncements(ctx, cfg)
	if errFetch != nil {
		return jsonResponse(http.StatusOK, map[string]any{"success": false, "message": errFetch.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]any{"success": true, "announcements": items})
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

var beijingOnce = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()

func beijingLocation() *time.Location { return beijingOnce }

// mergeOverrides folds per-request credential overrides sent by the dashboard
// into the stored configuration. Empty fields always mean "keep the
// configured value" so a page never has to echo stored secrets.
func mergeOverrides(cfg pluginConfig, body []byte) pluginConfig {
	if len(bytes.TrimSpace(body)) == 0 {
		return cfg
	}
	var payload struct {
		APIKey    string `json:"api_key"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		BaseURL   string `json:"base_url"`
		UserAgent string `json:"user_agent"`
		ProxyURL  string `json:"proxy_url"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return cfg
	}
	if value := strings.TrimSpace(payload.APIKey); value != "" {
		cfg.APIKey = value
	}
	if value := strings.TrimSpace(payload.Username); value != "" {
		cfg.Username = value
	}
	if value := strings.TrimSpace(payload.Password); value != "" {
		cfg.Password = value
	}
	if value := strings.TrimSpace(payload.BaseURL); value != "" {
		cfg.BaseURL = normalizeBaseURL(value)
	}
	if value := strings.TrimSpace(payload.UserAgent); value != "" {
		cfg.UserAgent = value
	}
	if value := strings.TrimSpace(payload.ProxyURL); value != "" {
		cfg.ProxyURL = value
	}
	return cfg
}

// probeModel executes one tiny inference request and returns the raw body.
func probeModel(ctx context.Context, cfg pluginConfig, model, protocol string) ([]byte, int, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, 0, fmt.Errorf("api_key is not configured")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	var endpoint string
	var payload []byte
	switch protocol {
	case protocolResponses:
		endpoint = "/v1/responses"
		payload = []byte(fmt.Sprintf(`{"model":%q,"input":"Reply with OK only","max_output_tokens":32}`, model))
	case protocolAnthropic:
		endpoint = "/v1/messages"
		payload = []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"Reply with OK only"}]}`, model))
	default:
		endpoint = "/v1/chat/completions"
		payload = []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"Reply with OK only"}],"stream":false}`, model))
	}
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodPost, base+endpoint, bytes.NewReader(payload))
	if errRequest != nil {
		return nil, 0, errRequest
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", cfg.UserAgent)
	if protocol == protocolAnthropic {
		request.Header.Set("x-api-key", cfg.APIKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	response, errDo := controlPlaneClient(cfg).Do(request)
	if errDo != nil {
		return nil, 0, errDo
	}
	defer response.Body.Close()
	responseBody, errRead := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if errRead != nil {
		return nil, response.StatusCode, errRead
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseBody, response.StatusCode, newUpstreamError(response.StatusCode, summarizeUpstreamError(responseBody))
	}
	return responseBody, response.StatusCode, nil
}
