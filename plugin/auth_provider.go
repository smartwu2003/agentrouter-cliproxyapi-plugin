package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// pendingLogin is one in-flight authorization started from the plugin card's
// "账号授权" button. The user finishes it in a browser on the plugin's own
// localhost page; PollLogin then hands the captured credentials to the host.
type pendingLogin struct {
	state     string
	createdAt time.Time
	expiresAt time.Time
	status    pluginapi.AuthLoginStatus
	message   string
	result    flowResult
	server    *http.Server
	listener  net.Listener
}

type loginManager struct {
	mu      sync.Mutex
	pending map[string]*pendingLogin
}

var logins = &loginManager{pending: map[string]*pendingLogin{}}

const loginTTL = 15 * time.Minute

func randomState() string {
	buf := make([]byte, 16)
	if _, errRead := rand.Read(buf); errRead != nil {
		return fmt.Sprintf("agentrouter-%d", time.Now().UnixNano())
	}
	return "agentrouter-" + hex.EncodeToString(buf)
}

func (m *loginManager) create() (*pendingLogin, error) {
	state := randomState()
	entry := &pendingLogin{
		state:     state,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(loginTTL),
		status:    pluginapi.AuthLoginStatusPending,
	}
	m.mu.Lock()
	m.pruneLocked()
	m.pending[state] = entry
	m.mu.Unlock()
	return entry, nil
}

func (m *loginManager) pruneLocked() {
	now := time.Now()
	for state, entry := range m.pending {
		if now.After(entry.expiresAt) {
			if entry.server != nil {
				go entry.server.Close()
			}
			delete(m.pending, state)
		}
	}
}

func (m *loginManager) get(state string) (*pendingLogin, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.pending[state]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry, true
}

func (m *loginManager) complete(state string, flow flowResult) {
	m.mu.Lock()
	entry, ok := m.pending[state]
	if ok {
		entry.status = pluginapi.AuthLoginStatusSuccess
		entry.result = flow
		entry.message = flow.Message
	}
	m.mu.Unlock()
	if ok && entry.server != nil {
		go func() {
			time.Sleep(2 * time.Second)
			_ = entry.server.Close()
		}()
	}
}

func (m *loginManager) fail(state, message string) {
	m.mu.Lock()
	entry, ok := m.pending[state]
	if ok {
		entry.status = pluginapi.AuthLoginStatusError
		entry.message = message
	}
	m.mu.Unlock()
	if ok && entry.server != nil {
		go entry.server.Close()
	}
}

// startLocalLoginServer serves the plugin's authorization form on 127.0.0.1.
// A random port and the one-time state keep the page bound to this flow.
func startLocalLoginServer(entry *pendingLogin) (string, error) {
	listener, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		return "", errListen
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(renderLoginPage(entry.state)))
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			State    string `json:"state"`
			Username string `json:"username"`
			Password string `json:"password"`
			Token    string `json:"token"`
			UserID   string `json:"user_id"`
		}
		if errDecode := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); errDecode != nil {
			writeLoginJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求格式错误"})
			return
		}
		if body.State != entry.state {
			writeLoginJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "授权会话不匹配，请重新点击「账号授权」"})
			return
		}
		cfg := loadedConfig()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if strings.TrimSpace(body.Token) != "" {
			state, errSave := saveManualToken(cfg, body.Token, body.UserID, body.Username)
			if errSave != nil {
				writeLoginJSON(w, http.StatusOK, map[string]any{"ok": false, "message": errSave.Error()})
				return
			}
			flow := flowResult{Success: true, Message: "访问令牌已保存", Token: state.AccountToken, UserID: state.UserID, Username: state.Username}
			logins.complete(entry.state, flow)
			writeLoginJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "授权完成，可以关闭此页面并返回 EasyCLIProxyAPI"})
			return
		}
		if strings.TrimSpace(body.Username) == "" || strings.TrimSpace(body.Password) == "" {
			writeLoginJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "请输入账号和密码"})
			return
		}
		loginCfg := cfg
		loginCfg.Username = strings.TrimSpace(body.Username)
		loginCfg.Password = body.Password
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		flow, errAuth := authorizeAccount(ctx, loginCfg)
		if errAuth != nil {
			writeLoginJSON(w, http.StatusOK, map[string]any{"ok": false, "message": errAuth.Error()})
			return
		}
		applyFlowToState(loginCfg, flow)
		primeCatalogue(effectiveConfig())
		logins.complete(entry.state, flow)
		writeLoginJSON(w, http.StatusOK, map[string]any{"ok": true, "message": flow.Message + "，可以关闭此页面并返回 EasyCLIProxyAPI"})
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      90 * time.Second,
	}
	entry.listener = listener
	entry.server = server
	go func() {
		if errServe := server.Serve(listener); errServe != nil && errServe != http.ErrServerClosed {
			hostLog("warn", "agentrouter local login server stopped", map[string]any{"error": errServe.Error()})
		}
	}()
	return fmt.Sprintf("http://127.0.0.1:%d/?state=%s", listener.Addr().(*net.TCPAddr).Port, entry.state), nil
}

func writeLoginJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// saveManualToken stores a token pasted from the AgentRouter console
// (个人设置 -> 系统令牌) after validating it against the dashboard API.
func saveManualToken(cfg pluginConfig, token, userID, username string) (pluginState, error) {
	token = strings.TrimSpace(token)
	userID = strings.TrimSpace(userID)
	if token == "" {
		return pluginState{}, fmt.Errorf("访问令牌不能为空")
	}
	client := accountClient{cfg: cfg, token: token, userID: userID}
	if userID == "" {
		// Without a user id the New API endpoints reject the call; ask for it.
		return pluginState{}, fmt.Errorf("请同时填写用户 ID（个人设置页右上角 ID，例如 123456）")
	}
	user, errSelf := client.fetchSelf(context.Background())
	if errSelf != nil {
		return pluginState{}, fmt.Errorf("令牌校验失败：%w", errSelf)
	}
	state, errSave := updateState(cfg, func(state *pluginState) {
		state.AccountToken = token
		state.UserID = fmt.Sprint(user.ID)
		if strings.TrimSpace(username) != "" {
			state.Username = strings.TrimSpace(username)
		}
		account := &storedAccount{}
		if state.Account != nil {
			*account = *state.Account
		}
		account.AccountToken = token
		account.UserID = fmt.Sprint(user.ID)
		account.Username = firstNonEmptyString(user.Username, account.Username, username)
		account.DisplayName = firstNonEmptyString(user.DisplayName, account.DisplayName)
		account.Email = firstNonEmptyString(user.Email, account.Email)
		account.Group = firstNonEmptyString(user.Group, account.Group)
		account.Quota = int64(user.Quota)
		account.UsedQuota = int64(user.UsedQuota)
		account.RequestCount = user.RequestCount
		account.LoginAt = unixNow()
		state.Account = account
	})
	if errSave != nil {
		return pluginState{}, errSave
	}
	return state, nil
}

// saveSessionCookie stores a console session cookie (copied from the browser)
// after validating it. The session is enough for read-only dashboard calls
// when the account logs in through GitHub/LinuxDO OAuth and has no password.
func saveSessionCookie(cfg pluginConfig, session, userID string) (pluginState, error) {
	session = strings.TrimSpace(session)
	userID = strings.TrimSpace(userID)
	if session == "" {
		return pluginState{}, fmt.Errorf("会话 Cookie 不能为空")
	}
	if userID == "" {
		return pluginState{}, fmt.Errorf("请同时填写用户 ID（个人设置页的 ID，例如 123456）")
	}
	cookie := session
	if !strings.Contains(cookie, "=") {
		cookie = "session=" + cookie
	}
	client := accountClient{cfg: cfg, userID: userID, session: cookie}
	user, errSelf := client.fetchSelf(context.Background())
	if errSelf != nil {
		return pluginState{}, fmt.Errorf("会话校验失败：%w", errSelf)
	}
	return updateState(cfg, func(state *pluginState) {
		state.UserID = fmt.Sprint(user.ID)
		if state.Account == nil {
			state.Account = &storedAccount{}
		}
		state.Account.Session = cookie
		state.Account.UserID = fmt.Sprint(user.ID)
		state.Account.Username = firstNonEmptyString(user.Username, state.Account.Username)
		state.Account.DisplayName = firstNonEmptyString(user.DisplayName, state.Account.DisplayName)
		state.Account.Email = firstNonEmptyString(user.Email, state.Account.Email)
		state.Account.Group = firstNonEmptyString(user.Group, state.Account.Group)
		state.Account.Quota = int64(user.Quota)
		state.Account.UsedQuota = int64(user.UsedQuota)
		state.Account.RequestCount = user.RequestCount
		state.Account.LoginAt = unixNow()
	})
}

// authDataFromState builds the host credential record from the stored account.
func authDataFromState(state pluginState) pluginapi.AuthData {
	key := strings.TrimSpace(state.APIKey)
	token := strings.TrimSpace(state.AccountToken)
	userID := strings.TrimSpace(state.UserID)
	username := strings.TrimSpace(state.Username)
	if state.Account != nil {
		key = firstNonEmptyString(key, state.Account.APIKey)
		token = firstNonEmptyString(token, state.Account.AccountToken)
		userID = firstNonEmptyString(userID, state.Account.UserID)
		username = firstNonEmptyString(username, state.Account.Username)
	}
	storage := map[string]any{
		"type":       providerKey,
		"api_key":    key,
		"user_agent": defaultUserAgent,
		"base_url":   defaultBaseURL,
	}
	if token != "" {
		storage["account_token"] = token
	}
	if userID != "" {
		storage["user_id"] = userID
	}
	if username != "" {
		storage["username"] = username
	}
	raw, _ := json.Marshal(storage)
	label := firstNonEmptyString(username, "AgentRouter")
	return pluginapi.AuthData{
		Provider:    providerKey,
		ID:          providerKey + ".json",
		FileName:    providerKey + ".json",
		Label:       label,
		StorageJSON: raw,
		Metadata:    map[string]any{"type": providerKey, "username": username},
		Attributes:  map[string]string{"auth_kind": "api_key"},
	}
}

// startLogin begins an authorization flow for the host's plugin OAuth dialog.
func startLogin(raw []byte) ([]byte, error) {
	var req pluginapi.AuthLoginStartRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	entry, errCreate := logins.create()
	if errCreate != nil {
		return nil, errCreate
	}
	url, errServer := startLocalLoginServer(entry)
	if errServer != nil {
		logins.fail(entry.state, errServer.Error())
		return nil, errServer
	}
	return okEnvelope(pluginapi.AuthLoginStartResponse{
		Provider:  providerKey,
		URL:       url,
		State:     entry.state,
		ExpiresAt: entry.expiresAt,
		Metadata: map[string]any{
			"hint": "在打开的页面中登录 AgentRouter 账号，或粘贴系统令牌完成授权。",
		},
	})
}

// pollLogin reports the progress of one authorization flow.
func pollLogin(raw []byte) ([]byte, error) {
	var req pluginapi.AuthLoginPollRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	state := strings.TrimSpace(req.State)
	entry, ok := logins.get(state)
	if !ok {
		return okEnvelope(pluginapi.AuthLoginPollResponse{
			Status:  pluginapi.AuthLoginStatusError,
			Message: "授权会话不存在或已过期，请重新点击「账号授权」",
		})
	}
	logins.mu.Lock()
	status := entry.status
	message := entry.message
	logins.mu.Unlock()
	switch status {
	case pluginapi.AuthLoginStatusSuccess:
		return okEnvelope(pluginapi.AuthLoginPollResponse{
			Status:  pluginapi.AuthLoginStatusSuccess,
			Message: message,
			Auth:    authDataFromState(cachedState()),
		})
	case pluginapi.AuthLoginStatusError:
		return okEnvelope(pluginapi.AuthLoginPollResponse{Status: status, Message: message})
	default:
		return okEnvelope(pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: "等待在浏览器中完成授权"})
	}
}

// parseAuth claims auth files written for this plugin.
func parseAuth(raw []byte) ([]byte, error) {
	var req pluginapi.AuthParseRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	var stored map[string]any
	if errUnmarshal := json.Unmarshal(req.RawJSON, &stored); errUnmarshal != nil {
		return okEnvelope(pluginapi.AuthParseResponse{Handled: false})
	}
	authType, _ := stored["type"].(string)
	provider, _ := stored["provider"].(string)
	if !strings.EqualFold(strings.TrimSpace(authType), providerKey) && !strings.EqualFold(strings.TrimSpace(provider), providerKey) {
		return okEnvelope(pluginapi.AuthParseResponse{Handled: false})
	}
	username, _ := stored["username"].(string)
	label := firstNonEmptyString(username, req.FileName, "AgentRouter")
	return okEnvelope(pluginapi.AuthParseResponse{
		Handled: true,
		Auth: pluginapi.AuthData{
			Provider:    providerKey,
			ID:          firstNonEmptyString(req.FileName, providerKey+".json"),
			FileName:    req.FileName,
			Label:       label,
			StorageJSON: append([]byte(nil), req.RawJSON...),
			Metadata:    map[string]any{"type": providerKey},
			Attributes:  map[string]string{"auth_kind": "api_key"},
		},
	})
}

// refreshAuth keeps the stored credential in sync with the persisted account.
func refreshAuth(raw []byte) ([]byte, error) {
	state := cachedState()
	data := authDataFromState(state)
	data.NextRefreshAfter = time.Now().Add(24 * time.Hour)
	return okEnvelope(pluginapi.AuthRefreshResponse{
		Auth:             data,
		NextRefreshAfter: data.NextRefreshAfter,
	})
}

// renderLoginPage is the form served by the temporary localhost server.
func renderLoginPage(state string) string {
	replacer := strings.NewReplacer(
		"{{STATE}}", html.EscapeString(state),
		"{{VERSION}}", html.EscapeString(pluginVersion),
	)
	return replacer.Replace(loginPageTemplate)
}

const loginPageTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgentRouter 账号授权</title>
<style>
  :root { color-scheme: light dark; --bg:#f6f7f9; --card:#fff; --text:#1f2328; --muted:#6b7280; --border:#e5e7eb; --accent:#4f46e5; --ok:#16a34a; --err:#dc2626; }
  @media (prefers-color-scheme: dark) { :root { --bg:#15171c; --card:#1e2127; --text:#e8eaf0; --muted:#9aa1ad; --border:#2d3138; --accent:#818cf8; --ok:#4ade80; --err:#f87171; } }
  * { box-sizing:border-box; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center; padding:24px; font:14px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif; background:var(--bg); color:var(--text); }
  .card { width:100%; max-width:420px; background:var(--card); border:1px solid var(--border); border-radius:14px; padding:24px; }
  h1 { margin:0 0 4px; font-size:18px; }
  p { margin:0 0 14px; color:var(--muted); font-size:13px; }
  label { display:block; margin:12px 0 4px; font-size:12px; color:var(--muted); }
  input { width:100%; padding:9px 11px; border:1px solid var(--border); border-radius:8px; background:var(--bg); color:var(--text); font-size:14px; }
  button { width:100%; margin-top:16px; padding:10px 14px; border:0; border-radius:9px; background:var(--accent); color:#fff; font-size:14px; cursor:pointer; }
  button:disabled { opacity:.6; cursor:progress; }
  .tabs { display:flex; gap:6px; margin:6px 0 4px; }
  .tabs button { margin:0; width:auto; flex:1; padding:7px; background:transparent; color:var(--muted); border:1px solid var(--border); }
  .tabs button.active { background:var(--accent); color:#fff; border-color:var(--accent); }
  .status { margin-top:14px; font-size:13px; min-height:20px; }
  .ok { color:var(--ok); } .err { color:var(--err); }
  .hint { margin-top:14px; padding-top:12px; border-top:1px dashed var(--border); font-size:12px; color:var(--muted); }
  a { color:var(--accent); }
</style>
</head>
<body>
<div class="card">
  <h1>AgentRouter 账号授权</h1>
  <p>授权后插件可以读取余额、使用日志、模型状态并自动签到。凭据只保存在本机。</p>
  <div class="tabs">
    <button type="button" id="tabLogin" class="active">账号密码登录</button>
    <button type="button" id="tabToken">粘贴系统令牌</button>
  </div>
  <form id="loginForm">
    <label>账号（用户名 / 邮箱）</label>
    <input id="username" autocomplete="username" required>
    <label>密码</label>
    <input id="password" type="password" autocomplete="current-password" required>
    <button type="submit" id="submitBtn">登录并授权</button>
  </form>
  <form id="tokenForm" style="display:none">
    <label>系统令牌</label>
    <input id="token" placeholder="在 AgentRouter 个人设置 → 系统令牌 获取">
    <label>用户 ID</label>
    <input id="userID" placeholder="个人设置页的 ID，例如 123456">
    <button type="submit" id="tokenBtn">保存令牌</button>
  </form>
  <div id="status" class="status"></div>
  <div class="hint">
    登录遇到图形验证码（Turnstile）时，请改用「粘贴系统令牌」：在
    <a href="https://ps.air-outer.com/console/personal" target="_blank" rel="noreferrer">AgentRouter 个人设置</a>
    页面复制系统令牌与用户 ID。授权页由插件 v{{VERSION}} 在本机临时提供。
  </div>
</div>
<script>
const STATE = '{{STATE}}';
const statusEl = document.getElementById('status');
const loginForm = document.getElementById('loginForm');
const tokenForm = document.getElementById('tokenForm');
const tabLogin = document.getElementById('tabLogin');
const tabToken = document.getElementById('tabToken');
function setStatus(text, ok) { statusEl.textContent = text; statusEl.className = 'status ' + (ok ? 'ok' : 'err'); }
function switchTab(token) {
  tabLogin.classList.toggle('active', !token);
  tabToken.classList.toggle('active', token);
  loginForm.style.display = token ? 'none' : '';
  tokenForm.style.display = token ? '' : 'none';
  setStatus('', true);
}
tabLogin.addEventListener('click', () => switchTab(false));
tabToken.addEventListener('click', () => switchTab(true));
async function submit(payload, button) {
  button.disabled = true;
  setStatus('正在连接 AgentRouter…', true);
  try {
    const response = await fetch('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(Object.assign({ state: STATE }, payload)),
    });
    const data = await response.json().catch(() => ({ ok: false, message: 'HTTP ' + response.status }));
    if (data.ok) { setStatus(data.message || '授权完成', true); loginForm.style.display = 'none'; tokenForm.style.display = 'none'; return; }
    setStatus(data.message || '授权失败', false);
  } catch (error) {
    setStatus('请求失败：' + error.message, false);
  } finally {
    button.disabled = false;
  }
}
loginForm.addEventListener('submit', event => {
  event.preventDefault();
  submit({ username: document.getElementById('username').value, password: document.getElementById('password').value }, document.getElementById('submitBtn'));
});
tokenForm.addEventListener('submit', event => {
  event.preventDefault();
  submit({ token: document.getElementById('token').value, user_id: document.getElementById('userID').value }, document.getElementById('tokenBtn'));
});
</script>
</body>
</html>
`
