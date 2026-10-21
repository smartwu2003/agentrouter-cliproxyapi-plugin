package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// accountClient speaks the New API dashboard endpoints of AgentRouter.
//
// Two credential shapes are supported:
//   - an access token (Authorization: Bearer) plus the numeric New-Api-User id,
//     which the console's "系统令牌" settings panel exposes and which the plugin
//     obtains from a console login;
//   - a browser session cookie obtained from a username/password login (or
//     captured by the authorization flow), needed only while an access token
//     has not been provisioned yet.
type accountClient struct {
	cfg     pluginConfig
	token   string
	userID  string
	session string
}

func newAccountClient(cfg pluginConfig) accountClient {
	client := accountClient{
		cfg:    cfg,
		token:  strings.TrimSpace(cfg.AccountToken),
		userID: strings.TrimSpace(cfg.UserID),
	}
	if state := cachedState(); state.Account != nil {
		if client.token == "" {
			client.token = strings.TrimSpace(state.Account.AccountToken)
		}
		if client.userID == "" {
			client.userID = strings.TrimSpace(state.Account.UserID)
		}
		client.session = strings.TrimSpace(state.Account.Session)
	}
	return client
}

func (c accountClient) hasCredentials() bool {
	return c.token != "" || c.session != ""
}

// endpoint builds an absolute dashboard URL.
func (c accountClient) endpoint(path string, query url.Values) string {
	target := accountBaseURL(c.cfg) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return target
}

// accountBaseURL returns the console origin for account/dashboard calls.
func accountBaseURL(cfg pluginConfig) string {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.DashboardBaseURL), "/")
	if base == "" {
		base = defaultDashboardURL
	}
	return base
}

// controlEndpoint builds a control-plane URL on the configured console origin.
func controlEndpoint(cfg pluginConfig, path string) string {
	return accountBaseURL(cfg) + path
}

// isWAFChallenge reports whether a response is the Aliyun WAF interstitial
// instead of JSON. The mainland mirror (ps.air-outer.com) serves it for
// scripted dashboard calls at random; the account client retries the console
// origin when it sees one.
func isWAFChallenge(body []byte) bool {
	return bytes.Contains(body, []byte("aliyun_waf")) || bytes.Contains(body, []byte("<html"))
}

// do performs one dashboard call and returns the decoded JSON body.
func (c accountClient) do(ctx context.Context, method, path string, query url.Values, body any) (map[string]any, error) {
	if !c.hasCredentials() {
		return nil, errAccountNotAuthorized
	}
	var reader io.Reader
	if body != nil {
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return nil, errMarshal
		}
		reader = bytes.NewReader(encoded)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	request, errRequest := http.NewRequestWithContext(reqCtx, method, c.endpoint(path, query), reader)
	if errRequest != nil {
		return nil, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", firstNonEmptyString(c.cfg.UserAgent, defaultUserAgent))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.userID != "" {
		request.Header.Set("New-Api-User", c.userID)
	}
	if c.session != "" {
		request.Header.Set("Cookie", c.session)
	}
	response, errDo := controlPlaneClient(c.cfg).Do(request)
	if errDo != nil {
		return nil, errDo
	}
	raw, errRead := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	response.Body.Close()
	if errRead != nil {
		return nil, errRead
	}
	if isWAFChallenge(raw) {
		// The console answered with an Aliyun WAF interstitial. It is triggered
		// by request bursts and clears after a short cooldown; retry on a fresh
		// connection and, if that still fails, on the alternate origin.
		for _, attempt := range []struct {
			delay time.Duration
			host  string
		}{
			{delay: 1200 * time.Millisecond, host: ""},
			{delay: 800 * time.Millisecond, host: alternateAccountHost(c.cfg)},
		} {
			if attempt.delay > 0 {
				select {
				case <-reqCtx.Done():
					return nil, reqCtx.Err()
				case <-time.After(attempt.delay):
				}
			}
			target := request.URL.String()
			if attempt.host != "" {
				target = replaceURLHost(target, attempt.host)
			}
			retry, errRetry := http.NewRequestWithContext(reqCtx, method, target, bytes.NewReader(encodedBody(body)))
			if errRetry != nil {
				continue
			}
			retry.Header = request.Header.Clone()
			if attempt.host != "" {
				// Keep any browser-session cookie scoped to the current host only.
				retry.Header.Del("Cookie")
			}
			retried, errDoRetry := controlPlaneClient(c.cfg).Do(retry)
			if errDoRetry != nil {
				continue
			}
			rawRetry, errReadRetry := io.ReadAll(io.LimitReader(retried.Body, 8<<20))
			retried.Body.Close()
			if errReadRetry != nil || isWAFChallenge(rawRetry) {
				continue
			}
			raw = rawRetry
			response = retried
			break
		}
		if isWAFChallenge(raw) {
			return nil, fmt.Errorf("AgentRouter 控制台触发了 WAF 风控（HTTP %d），请稍后重试；如长期失败，请在插件设置中配置 proxy_url 并通过 agentrouter.org 访问", response.StatusCode)
		}
	}
	var payload map[string]any
	if errUnmarshal := json.Unmarshal(raw, &payload); errUnmarshal != nil {
		if response.StatusCode != http.StatusOK {
			return nil, newUpstreamError(response.StatusCode, summarizeUpstreamError(raw))
		}
		return nil, fmt.Errorf("agentrouter dashboard returned non-JSON response")
	}
	if response.StatusCode != http.StatusOK {
		message := summarizeUpstreamError(raw)
		if message == "" {
			message = fmt.Sprintf("AgentRouter dashboard returned HTTP %d", response.StatusCode)
		}
		if success, ok := payload["success"].(bool); ok && !success {
			return payload, newUpstreamError(response.StatusCode, message)
		}
		return payload, newUpstreamError(response.StatusCode, message)
	}
	if success, ok := payload["success"].(bool); ok && !success {
		message, _ := payload["message"].(string)
		return payload, fmt.Errorf("%s", strings.TrimSpace(message))
	}
	return payload, nil
}

// alternateAccountHost returns the other console origin to try after a WAF block.
func alternateAccountHost(cfg pluginConfig) string {
	base := accountBaseURL(cfg)
	if strings.Contains(base, "agentrouter.org") {
		return defaultBaseURL
	}
	return "https://agentrouter.org"
}

// replaceURLHost swaps the scheme+host of an absolute URL.
func replaceURLHost(raw, host string) string {
	parsed, errParse := url.Parse(raw)
	if errParse != nil {
		return raw
	}
	alternate, errAlt := url.Parse(host)
	if errAlt != nil {
		return raw
	}
	parsed.Scheme = alternate.Scheme
	parsed.Host = alternate.Host
	return parsed.String()
}

// encodedBody re-encodes a request body for a retry.
func encodedBody(body any) []byte {
	if body == nil {
		return nil
	}
	encoded, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		return nil
	}
	return encoded
}

var errAccountNotAuthorized = fmt.Errorf("AgentRouter 账号尚未授权：请在插件页面登录账号，或在插件配置中填写 account_token 与 user_id")

// errAccountSessionOnly marks calls that need a browser session instead of a
// scoped access token.
var errAccountSessionOnly = fmt.Errorf("this AgentRouter endpoint needs a browser session; perform the account authorization flow")

// selfUser is the subset of /api/user/self the dashboard shows.
type selfUser struct {
	ID           int64   `json:"id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	Email        string  `json:"email"`
	Group        string  `json:"group"`
	Quota        float64 `json:"quota"`
	UsedQuota    float64 `json:"used_quota"`
	RequestCount int64   `json:"request_count"`
	CreatedAt    int64   `json:"created_at"`
	LastLoginAt  int64   `json:"last_login_at"`
}

func (c accountClient) fetchSelf(ctx context.Context) (selfUser, error) {
	payload, errCall := c.do(ctx, http.MethodGet, "/api/user/self", nil, nil)
	if errCall != nil {
		return selfUser{}, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var user selfUser
	if errUnmarshal := json.Unmarshal(raw, &user); errUnmarshal != nil {
		return selfUser{}, fmt.Errorf("decode account profile: %w", errUnmarshal)
	}
	return user, nil
}

func (c accountClient) fetchModels(ctx context.Context) ([]string, error) {
	payload, errCall := c.do(ctx, http.MethodGet, "/api/user/models", nil, nil)
	if errCall != nil {
		return nil, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var models []string
	if errUnmarshal := json.Unmarshal(raw, &models); errUnmarshal != nil {
		return nil, fmt.Errorf("decode account models: %w", errUnmarshal)
	}
	return models, nil
}

func (c accountClient) fetchGroups(ctx context.Context) (map[string]any, error) {
	payload, errCall := c.do(ctx, http.MethodGet, "/api/user/self/groups", nil, nil)
	if errCall != nil {
		return nil, errCall
	}
	groups, _ := payload["data"].(map[string]any)
	return groups, nil
}

func (c accountClient) fetchAccessToken(ctx context.Context) (string, error) {
	payload, errCall := c.do(ctx, http.MethodGet, "/api/user/token", nil, nil)
	if errCall != nil {
		return "", errCall
	}
	token, _ := payload["data"].(string)
	return strings.TrimSpace(token), nil
}

// listTokensAll returns the API keys of the account using the most recently
// updated page first; pageSize is clamped by the upstream API.
func (c accountClient) listTokens(ctx context.Context, pageSize int) ([]apiToken, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	query := url.Values{}
	query.Set("p", "0")
	query.Set("size", strconv.Itoa(pageSize))
	payload, errCall := c.do(ctx, http.MethodGet, "/api/token/", query, nil)
	if errCall != nil {
		return nil, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var page struct {
		Items []apiToken `json:"items"`
		Total int        `json:"total"`
	}
	if errUnmarshal := json.Unmarshal(raw, &page); errUnmarshal != nil {
		return nil, fmt.Errorf("decode token list: %w", errUnmarshal)
	}
	return page.Items, nil
}

func (c accountClient) createToken(ctx context.Context, name string) (apiToken, error) {
	body := map[string]any{
		"name":                 name,
		"remain_quota":         0,
		"expired_time":         -1,
		"unlimited_quota":      true,
		"model_limits_enabled": false,
		"model_limits":         "",
		"allow_ips":            "",
		"group":                "",
	}
	if _, errCall := c.do(ctx, http.MethodPost, "/api/token/", nil, body); errCall != nil {
		return apiToken{}, errCall
	}
	tokens, errList := c.listTokens(ctx, 100)
	if errList != nil {
		return apiToken{}, errList
	}
	for _, token := range tokens {
		if token.Name == name {
			return token, nil
		}
	}
	return apiToken{}, fmt.Errorf("token %q was created but cannot be found in the token list", name)
}

func (c accountClient) fetchCheckinStatus(ctx context.Context, month string) (map[string]any, error) {
	query := url.Values{}
	if month != "" {
		query.Set("month", month)
	}
	payload, errCall := c.do(ctx, http.MethodGet, "/api/user/self/checkin", query, nil)
	if errCall != nil {
		return nil, errCall
	}
	data, _ := payload["data"].(map[string]any)
	return data, nil
}

func (c accountClient) checkin(ctx context.Context) (map[string]any, error) {
	payload, errCall := c.do(ctx, http.MethodPost, "/api/user/self/checkin", nil, map[string]any{})
	if errCall != nil {
		return nil, errCall
	}
	data, _ := payload["data"].(map[string]any)
	return data, nil
}

func (c accountClient) fetchLogs(ctx context.Context, logType int, page, pageSize int, model string, start, end int64) (logPage, error) {
	query := url.Values{}
	query.Set("p", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))
	if logType >= 0 {
		query.Set("type", strconv.Itoa(logType))
	}
	if model != "" {
		query.Set("model_name", model)
	}
	if start > 0 {
		query.Set("start_timestamp", strconv.FormatInt(start, 10))
	}
	if end > 0 {
		query.Set("end_timestamp", strconv.FormatInt(end, 10))
	}
	payload, errCall := c.do(ctx, http.MethodGet, "/api/log/self", query, nil)
	if errCall != nil {
		return logPage{}, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var pageData logPage
	if errUnmarshal := json.Unmarshal(raw, &pageData); errUnmarshal != nil {
		return logPage{}, fmt.Errorf("decode usage log: %w", errUnmarshal)
	}
	return pageData, nil
}

func (c accountClient) fetchLogStat(ctx context.Context, start, end int64, model string) (map[string]float64, error) {
	query := url.Values{}
	query.Set("type", "0")
	if start > 0 {
		query.Set("start_timestamp", strconv.FormatInt(start, 10))
	}
	if end > 0 {
		query.Set("end_timestamp", strconv.FormatInt(end, 10))
	}
	if model != "" {
		query.Set("model_name", model)
	}
	payload, errCall := c.do(ctx, http.MethodGet, "/api/log/self/stat", query, nil)
	if errCall != nil {
		return nil, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var stat map[string]float64
	if errUnmarshal := json.Unmarshal(raw, &stat); errUnmarshal != nil {
		return nil, fmt.Errorf("decode usage stat: %w", errUnmarshal)
	}
	return stat, nil
}

func (c accountClient) fetchDashboard(ctx context.Context, start, end int64, defaultTime string) ([]dashboardPoint, error) {
	query := url.Values{}
	query.Set("start_timestamp", strconv.FormatInt(start, 10))
	query.Set("end_timestamp", strconv.FormatInt(end, 10))
	if defaultTime == "" {
		defaultTime = "day"
	}
	query.Set("default_time", defaultTime)
	payload, errCall := c.do(ctx, http.MethodGet, "/api/data/self", query, nil)
	if errCall != nil {
		return nil, errCall
	}
	raw, _ := json.Marshal(payload["data"])
	var points []dashboardPoint
	if errUnmarshal := json.Unmarshal(raw, &points); errUnmarshal != nil {
		return nil, fmt.Errorf("decode dashboard data: %w", errUnmarshal)
	}
	return points, nil
}

// apiToken is one row of /api/token/.
type apiToken struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`
	Key            string `json:"key"`
	Status         int    `json:"status"`
	Name           string `json:"name"`
	CreatedTime    int64  `json:"created_time"`
	AccessedTime   int64  `json:"accessed_time"`
	ExpiredTime    int64  `json:"expired_time"`
	RemainQuota    int64  `json:"remain_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	UsedQuota      int64  `json:"used_quota"`
	Group          string `json:"group"`
}

// findUsableToken prefers an existing unlimited token with a key so the
// plugin can reuse the key the user already created in the console.
func findUsableToken(tokens []apiToken, preferredName string) (apiToken, bool) {
	for _, token := range tokens {
		if token.Name == preferredName && strings.TrimSpace(token.Key) != "" && token.Status == 1 {
			return token, true
		}
	}
	for _, token := range tokens {
		if strings.TrimSpace(token.Key) == "" || token.Status != 1 {
			continue
		}
		if token.ExpiredTime == -1 || token.ExpiredTime > unixNow() {
			return token, true
		}
	}
	return apiToken{}, false
}

// quotaToUSD converts New API quota units to the displayed dollar amount.
func quotaToUSD(value float64) float64 {
	return value / quotaPerUnit
}
