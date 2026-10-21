package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
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
)

func managementRegistration() managementRegistrationResponse {
	return managementRegistrationResponse{
		Resources: []pluginapi.ResourceRoute{
			{
				Path:        resourceDashboardPath,
				Menu:        "AgentRouter",
				Description: "AgentRouter account, model catalogue and daily check-in.",
			},
		},
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodPost, Path: routeRefreshModels},
			{Method: http.MethodGet, Path: routeAccount},
			{Method: http.MethodPost, Path: routeCheckin},
			{Method: http.MethodPost, Path: routeTest},
			{Method: http.MethodGet, Path: routeModels},
			{Method: http.MethodGet, Path: routeSettings},
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
			Body:       []byte(renderDashboard(loadedConfig())),
		})
	case routeModels:
		return okEnvelope(modelsResponse())
	case routeSettings:
		return okEnvelope(settingsResponse())
	case routeRefreshModels:
		return okEnvelope(refreshModelsResponse(req))
	case routeAccount:
		return okEnvelope(accountResponse(req))
	case routeCheckin:
		return okEnvelope(checkinResponse(req))
	case routeTest:
		return okEnvelope(testResponse(req))
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
	cfg := loadedConfig()
	models := resolveModels(cfg)
	out := make([]map[string]any, 0, len(models))
	for _, spec := range models {
		out = append(out, map[string]any{
			"name":                  spec.Name,
			"client_name":           modelClientName(spec),
			"display_name":          spec.DisplayName,
			"protocol":              protocolForModel(cfg, spec.Name),
			"protocols":             spec.Protocols,
			"context_length":        spec.ContextLength,
			"max_completion_tokens": spec.MaxCompletionTokens,
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
	cfg := loadedConfig()
	return jsonResponse(http.StatusOK, map[string]any{
		"success":              true,
		"base_url":             cfg.BaseURL,
		"user_agent":           cfg.UserAgent,
		"proxy_url":            cfg.ProxyURL,
		"auto_discover_models": autoDiscoverModels(cfg),
		"has_api_key":          strings.TrimSpace(cfg.APIKey) != "",
		"has_account":          strings.TrimSpace(cfg.Username) != "" && strings.TrimSpace(cfg.Password) != "",
		"username":             cfg.Username,
		"checkin_on_startup":   cfg.CheckinOnStartup,
		"checkin_hour":         cfg.CheckinHour,
	})
}

func refreshModelsResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(loadedConfig(), req.Body)
	if strings.TrimSpace(cfg.APIKey) == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"success": false, "message": "api_key is not configured"})
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
	cfg := mergeOverrides(loadedConfig(), req.Body)
	info, errInfo := fetchAccountInfo(context.Background(), cfg)
	if errInfo != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"success": false, "message": errInfo.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]any{"success": true, "account": info})
}

func checkinResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(loadedConfig(), req.Body)
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

func testResponse(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	cfg := mergeOverrides(loadedConfig(), req.Body)
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

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

// mergeOverrides folds per-request credential overrides sent by the dashboard
// into the stored configuration. The dashboard is never sent stored secrets
// back, so an empty field always means "keep the configured value".
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

// renderDashboard builds the self-contained browser resource page.
func renderDashboard(cfg pluginConfig) string {
	return strings.NewReplacer(
		"{{BASE_URL}}", html.EscapeString(cfg.BaseURL),
		"{{USER_AGENT}}", html.EscapeString(cfg.UserAgent),
		"{{PLUGIN_VERSION}}", pluginVersion,
	).Replace(dashboardTemplate)
}

const dashboardTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgentRouter</title>
<style>
  :root { color-scheme: light dark; --bg:#f5f6f8; --card:#fff; --text:#1f2328; --muted:#6b7280; --border:#e5e7eb; --accent:#4f46e5; --ok:#16a34a; --err:#dc2626; }
  @media (prefers-color-scheme: dark) { :root { --bg:#16181d; --card:#1f2228; --text:#e8eaf0; --muted:#9aa1ad; --border:#2d3138; --accent:#818cf8; --ok:#4ade80; --err:#f87171; } }
  * { box-sizing:border-box; }
  body { margin:0; padding:18px; font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif; background:var(--bg); color:var(--text); }
  h1 { font-size:18px; margin:0 0 4px; } h2 { font-size:15px; margin:0 0 10px; }
  .muted { color:var(--muted); font-size:12px; }
  .grid { display:grid; gap:14px; grid-template-columns:repeat(auto-fit,minmax(320px,1fr)); }
  .card { background:var(--card); border:1px solid var(--border); border-radius:12px; padding:16px; }
  .row { display:flex; gap:8px; flex-wrap:wrap; align-items:center; }
  button { cursor:pointer; border:1px solid var(--border); background:var(--card); color:var(--text); border-radius:8px; padding:7px 14px; font-size:13px; }
  button.primary { background:var(--accent); border-color:var(--accent); color:#fff; }
  button:disabled { opacity:.55; cursor:progress; }
  input { width:100%; padding:7px 10px; border:1px solid var(--border); border-radius:8px; background:var(--bg); color:var(--text); font-size:13px; }
  table { width:100%; border-collapse:collapse; font-size:13px; }
  th, td { text-align:left; padding:6px 8px; border-bottom:1px solid var(--border); vertical-align:top; }
  th { color:var(--muted); font-weight:600; font-size:12px; }
  .pill { display:inline-block; padding:1px 8px; border-radius:999px; background:var(--border); font-size:11px; margin-right:4px; }
  .ok { color:var(--ok); } .err { color:var(--err); }
  pre { white-space:pre-wrap; word-break:break-all; background:var(--bg); border-radius:8px; padding:10px; font-size:12px; max-height:220px; overflow:auto; }
  .stat { display:flex; justify-content:space-between; gap:12px; padding:5px 0; border-bottom:1px dashed var(--border); }
  .stat:last-child { border-bottom:none; }
  label { display:block; font-size:12px; color:var(--muted); margin:8px 0 3px; }
  .header { display:flex; justify-content:space-between; align-items:flex-end; gap:12px; margin-bottom:14px; flex-wrap:wrap; }
  .hint { font-size:12px; color:var(--muted); margin-top:6px; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>AgentRouter</h1>
    <div class="muted">API 网关 <span id="baseURL">{{BASE_URL}}</span> · 插件 v{{PLUGIN_VERSION}}</div>
  </div>
  <div class="row">
    <button id="refreshBtn" class="primary">刷新模型</button>
    <button id="checkinBtn">签到</button>
  </div>
</div>
<div class="grid">
  <section class="card">
    <h2>账号</h2>
    <div id="accountBox" class="muted">加载中…</div>
    <div class="row" style="margin-top:10px">
      <button id="accountBtn">查询账号</button>
    </div>
    <div id="accountForm" style="display:none;margin-top:10px">
      <label>账号（用户名 / 邮箱）</label><input id="usernameInput" autocomplete="username" placeholder="用于每日签到的账号">
      <label>密码</label><input id="passwordInput" type="password" autocomplete="current-password" placeholder="仅用于签到，不会被回显">
      <label>API Key（可选，覆盖插件配置）</label><input id="apiKeyInput" type="password" placeholder="留空使用插件配置中的 api_key">
      <div class="hint">账号与 API Key 只用于本次请求，不会写入磁盘。</div>
    </div>
  </section>
  <section class="card">
    <h2>模型（<span id="modelCount">0</span>）</h2>
    <div class="muted" id="modelMeta"></div>
    <div style="max-height:320px;overflow:auto;margin-top:8px">
      <table><thead><tr><th>模型</th><th>协议</th><th>上下文</th></tr></thead><tbody id="modelRows"></tbody></table>
    </div>
  </section>
  <section class="card">
    <h2>连通性</h2>
    <div class="muted">发送一条极小的测试请求，验证 User-Agent、密钥与协议选择。</div>
    <div style="margin-top:10px">
      <label>测试模型</label><input id="testModelInput" placeholder="留空使用列表中的第一个模型">
      <div class="row" style="margin-top:10px"><button id="testBtn">测试</button></div>
      <div id="testResult" style="margin-top:10px"></div>
    </div>
  </section>
  <section class="card">
    <h2>插件配置</h2>
    <div id="settingsBox" class="muted">加载中…</div>
    <div class="hint">修改配置请打开 EasyCLIProxyAPI 的插件配置或 config.yaml，刷新后本页会读取新的值。</div>
  </section>
</div>
<script>
const API = {
  refreshModels: '/v0/management/agentrouter/refresh-models',
  account: '/v0/management/agentrouter/account',
  checkin: '/v0/management/agentrouter/checkin',
  test: '/v0/management/agentrouter/test',
  models: '/v0/management/agentrouter/models',
  settings: '/v0/management/agentrouter/settings'
};
function managementKey() {
  try {
    const auth = JSON.parse(localStorage.getItem('cli-proxy-auth') || '{}');
    if (auth && auth.state && typeof auth.state.managementKey === 'string') return auth.state.managementKey;
  } catch (e) {}
  return '';
}
async function api(path, options) {
  const key = managementKey();
  const headers = Object.assign({ 'Content-Type': 'application/json' }, (options && options.headers) || {});
  if (key) headers['Authorization'] = 'Bearer ' + key;
  const response = await fetch(path, Object.assign({}, options || {}, { headers }));
  const text = await response.text();
  let payload = null;
  try { payload = text ? JSON.parse(text) : null; } catch (e) { payload = { success:false, message:text }; }
  if (!response.ok) throw new Error((payload && payload.message) || ('HTTP ' + response.status));
  return payload;
}
function credentials() {
  const data = {};
  const username = document.getElementById('usernameInput').value.trim();
  const password = document.getElementById('passwordInput').value;
  const apiKey = document.getElementById('apiKeyInput').value.trim();
  if (username) data.username = username;
  if (password) data.password = password;
  if (apiKey) data.api_key = apiKey;
  return data;
}
function show(el, html) { el.innerHTML = html; }
function esc(value) { return String(value == null ? '' : value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function renderAccount(info) {
  const box = document.getElementById('accountBox');
  if (!info) { show(box, '<span class="err">暂无账号数据</span>'); return; }
  if (info.error) { show(box, '<span class="err">' + esc(info.error) + '</span>'); return; }
  const rows = [];
  rows.push(['账号', esc(info.display_name || info.username || '-')]);
  rows.push(['分组', esc(info.group || '-')]);
  rows.push(['余额', info.quota_usd != null ? '$' + Number(info.quota_usd).toFixed(2) : '-']);
  rows.push(['已用', info.used_usd != null ? '$' + Number(info.used_usd).toFixed(2) : '-']);
  rows.push(['请求次数', esc(info.request_count != null ? info.request_count : '-')]);
  rows.push(['可用模型', esc(info.models && info.models.length ? info.models.join(', ') : '-')]);
  if (info.checked_in_today != null) rows.push(['今日签到', info.checked_in_today ? '<span class="ok">已签到</span>' : '<span class="err">未签到</span>']);
  show(box, rows.map(([k, v]) => '<div class="stat"><span class="muted">' + k + '</span><span>' + v + '</span></div>').join(''));
}
async function loadModels() {
  const payload = await api(API.models);
  const models = payload.models || [];
  document.getElementById('modelCount').textContent = String(models.length);
  document.getElementById('modelMeta').textContent = payload.last_sync ? ('上次同步：' + payload.last_sync + (payload.last_error ? ' · ' + payload.last_error : '')) : '尚未从 AgentRouter 同步过模型列表';
  document.getElementById('modelRows').innerHTML = models.map(model =>
    '<tr><td>' + esc(model.name) + '<div class="muted">' + esc(model.client_name) + '</div></td><td><span class="pill">' + esc(model.protocol) + '</span>' + (model.protocols || []).map(p => '<span class="pill">' + esc(p) + '</span>').join('') + '</td><td>' + (model.context_length ? Math.round(model.context_length / 1000) + 'k' : '-') + '</td></tr>'
  ).join('') || '<tr><td colspan="3" class="muted">暂无模型</td></tr>';
}
async function loadSettings() {
  const payload = await api(API.settings);
  document.getElementById('baseURL').textContent = payload.base_url || '';
  document.getElementById('settingsBox').innerHTML = [
    ['API 地址', payload.base_url],
    ['User-Agent', payload.user_agent],
    ['自动发现模型', payload.auto_discover_models ? '开启' : '关闭'],
    ['API Key', payload.has_api_key ? '已配置' : '未配置'],
    ['签到账号', payload.has_account ? (payload.username || '已配置') : '未配置'],
    ['启动时签到', payload.checkin_on_startup ? '开启' : '关闭'],
    ['签到时间（北京时间）', payload.checkin_hour + ':00']
  ].map(([k, v]) => '<div class="stat"><span class="muted">' + k + '</span><span>' + esc(v) + '</span></div>').join('');
}
async function queryAccount() {
  renderAccount({ error: '查询中…' });
  try {
    const payload = await api(API.account);
    renderAccount(payload.account);
  } catch (error) {
    renderAccount({ error: error.message });
  }
}
async function refreshModels() {
  const button = document.getElementById('refreshBtn');
  button.disabled = true;
  try {
    const payload = await api(API.refreshModels, { method: 'POST', body: JSON.stringify(credentials()) });
    await loadModels();
    await loadSettings();
    alert('已同步 ' + payload.count + ' 个模型');
  } catch (error) {
    alert('刷新失败：' + error.message);
  } finally {
    button.disabled = false;
  }
}
async function checkin() {
  const button = document.getElementById('checkinBtn');
  button.disabled = true;
  try {
    const payload = await api(API.checkin, { method: 'POST', body: JSON.stringify(credentials()) });
    alert((payload.result && payload.result.message) || '签到成功');
    await queryAccount();
  } catch (error) {
    alert('签到失败：' + error.message);
  } finally {
    button.disabled = false;
  }
}
async function runTest() {
  const button = document.getElementById('testBtn');
  button.disabled = true;
  const resultBox = document.getElementById('testResult');
  resultBox.textContent = '测试中…';
  try {
    const body = credentials();
    body.model = document.getElementById('testModelInput').value.trim();
    const payload = await api(API.test, { method: 'POST', body: JSON.stringify(body) });
    show(resultBox, '<div class="ok">' + esc(payload.model) + ' · ' + esc(payload.protocol) + ' · ' + esc(payload.latency_ms) + 'ms</div><pre>' + esc(payload.response || '') + '</pre>');
  } catch (error) {
    show(resultBox, '<div class="err">' + esc(error.message) + '</div>');
  } finally {
    button.disabled = false;
  }
}
document.getElementById('refreshBtn').addEventListener('click', refreshModels);
document.getElementById('checkinBtn').addEventListener('click', checkin);
document.getElementById('accountBtn').addEventListener('click', queryAccount);
document.getElementById('testBtn').addEventListener('click', runTest);
if (!managementKey()) {
  document.getElementById('accountForm').style.display = '';
}
(async () => {
  try { await Promise.all([loadModels(), loadSettings()]); } catch (error) { console.error(error); }
  queryAccount();
})();
</script>
</body>
</html>
`
