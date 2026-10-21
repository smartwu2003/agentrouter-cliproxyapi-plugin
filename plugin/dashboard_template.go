package main

// dashboardTemplate is the single-page console rendered for the AgentRouter
// plugin resource. It is intentionally dependency-free: the GUI loads the
// page in a webview, so every asset must be inline and every call must use
// same-origin fetch with the management key the GUI injects.
const dashboardTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgentRouter</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #f5f6f8; --card: #ffffff; --text: #1f2328; --muted: #6b7280;
    --border: #e5e7eb; --accent: #4f46e5; --accent-soft: #eef2ff;
    --ok: #16a34a; --warn: #d97706; --err: #dc2626; --chart: #6366f1;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #15171c; --card: #1e2127; --text: #e8eaf0; --muted: #9aa1ad;
      --border: #2d3138; --accent: #818cf8; --accent-soft: #262a3a;
      --ok: #4ade80; --warn: #fbbf24; --err: #f87171; --chart: #818cf8;
    }
  }
  * { box-sizing: border-box; }
  body { margin: 0; padding: 18px; font: 14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC", "Microsoft YaHei", sans-serif; background: var(--bg); color: var(--text); }
  h1 { font-size: 18px; margin: 0; }
  h2 { font-size: 15px; margin: 0 0 12px; display: flex; align-items: center; gap: 8px; }
  h3 { font-size: 13px; margin: 14px 0 8px; color: var(--muted); font-weight: 600; }
  .muted { color: var(--muted); font-size: 12px; }
  .header { display: flex; justify-content: space-between; align-items: flex-end; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
  .header .status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--muted); margin-right: 6px; }
  .header .status-dot.on { background: var(--ok); }
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  .grid { display: grid; gap: 14px; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); }
  .grid.two { grid-template-columns: repeat(auto-fit, minmax(360px, 1fr)); }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 16px; }
  .tabs { display: flex; gap: 4px; margin: 0 0 14px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .tabs button { border: 0; background: transparent; color: var(--muted); padding: 8px 14px; font-size: 13px; cursor: pointer; border-bottom: 2px solid transparent; }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 600; }
  button { cursor: pointer; border: 1px solid var(--border); background: var(--card); color: var(--text); border-radius: 8px; padding: 7px 14px; font-size: 13px; }
  button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  button.small { padding: 4px 10px; font-size: 12px; }
  button:disabled { opacity: .55; cursor: progress; }
  input, select { width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); color: var(--text); font-size: 13px; }
  label { display: block; font-size: 12px; color: var(--muted); margin: 10px 0 4px; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid var(--border); vertical-align: top; }
  th { color: var(--muted); font-weight: 600; font-size: 12px; }
  .pill { display: inline-block; padding: 1px 8px; border-radius: 999px; background: var(--border); font-size: 11px; margin: 0 4px 2px 0; }
  .pill.ok { background: rgba(22,163,74,.15); color: var(--ok); }
  .pill.warn { background: rgba(217,119,6,.15); color: var(--warn); }
  .pill.err { background: rgba(220,38,38,.15); color: var(--err); }
  .ok { color: var(--ok); } .err { color: var(--err); } .warn { color: var(--warn); }
  pre { white-space: pre-wrap; word-break: break-all; background: var(--bg); border-radius: 8px; padding: 10px; font-size: 12px; max-height: 260px; overflow: auto; }
  .stat { display: flex; justify-content: space-between; gap: 12px; padding: 6px 0; border-bottom: 1px dashed var(--border); font-size: 13px; }
  .stat:last-child { border-bottom: none; }
  .metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 10px; }
  .metric { background: var(--accent-soft); border-radius: 10px; padding: 12px; }
  .metric .label { font-size: 12px; color: var(--muted); }
  .metric .value { font-size: 20px; font-weight: 650; margin-top: 4px; }
  .metric .sub { font-size: 12px; color: var(--muted); margin-top: 2px; }
  .empty { padding: 24px; text-align: center; color: var(--muted); font-size: 13px; }
  .banner { border: 1px solid var(--border); background: var(--accent-soft); border-radius: 10px; padding: 12px 14px; font-size: 13px; }
  .banner.error { background: rgba(220,38,38,.08); border-color: rgba(220,38,38,.25); }
  .banner.warn { background: rgba(217,119,6,.08); border-color: rgba(217,119,6,.25); }
  .scroll { max-height: 420px; overflow: auto; }
  .charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }
  .chart-title { font-size: 12px; color: var(--muted); margin-bottom: 8px; }
  .bars { display: flex; align-items: flex-end; gap: 4px; height: 130px; padding-top: 6px; }
  .bars .bar { flex: 1; min-width: 6px; background: var(--chart); border-radius: 3px 3px 0 0; opacity: .85; position: relative; }
  .bars .bar:hover { opacity: 1; }
  .bars .bar span { display: none; position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%); background: var(--card); border: 1px solid var(--border); border-radius: 6px; padding: 2px 6px; font-size: 11px; white-space: nowrap; z-index: 2; }
  .bars .bar:hover span { display: block; }
  .legend { display: flex; gap: 12px; font-size: 12px; color: var(--muted); margin-top: 6px; flex-wrap: wrap; }
  .legend i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 4px; }
  .kv { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; font-size: 13px; }
  .kv span:nth-child(odd) { color: var(--muted); }
  .split { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  @media (max-width: 720px) { .split { grid-template-columns: 1fr; } }
  .menu-item { cursor: pointer; }
  .menu-item:hover td { background: var(--accent-soft); }
  .toast { position: fixed; right: 18px; bottom: 18px; background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 10px 14px; font-size: 13px; box-shadow: 0 8px 24px rgba(0,0,0,.12); opacity: 0; transform: translateY(8px); transition: all .18s ease; pointer-events: none; z-index: 10; max-width: 360px; }
  .toast.show { opacity: 1; transform: translateY(0); }
  .toast.err { border-color: rgba(220,38,38,.5); color: var(--err); }
  .page { display: none; }
  .page.active { display: block; }
  .kv-form { display: grid; gap: 10px; }
  .hint { font-size: 12px; color: var(--muted); margin-top: 6px; }
  code { background: var(--bg); border-radius: 4px; padding: 1px 5px; font-size: 12px; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>AgentRouter</h1>
    <div class="muted"><span id="healthDot" class="status-dot"></span><span id="healthText">加载中…</span> · 网关 <span id="baseURL">{{BASE_URL}}</span> · 插件 v{{PLUGIN_VERSION}}</div>
  </div>
  <div class="row">
    <button id="refreshAll" class="primary">刷新全部</button>
    <button id="checkinBtn">立即签到</button>
  </div>
</div>

<div class="tabs">
  <button data-tab="overview" class="active">概览</button>
  <button data-tab="dashboard">数据看板</button>
  <button data-tab="models">模型</button>
  <button data-tab="logs">使用日志</button>
  <button data-tab="account">账号与授权</button>
  <button data-tab="settings">设置</button>
</div>

<section class="page active" id="page-overview">
  <div id="overviewBanner"></div>
  <div class="grid">
    <div class="card">
      <h2>账号</h2>
      <div class="metrics" id="overviewMetrics"></div>
      <div id="overviewAccount" style="margin-top:12px"></div>
      <div class="row" style="margin-top:10px">
        <button id="accountBtn">查询账号</button>
        <button id="checkinStatusBtn">签到状态</button>
      </div>
      <div id="checkinStatusBox" style="margin-top:10px"></div>
    </div>
    <div class="card">
      <h2>模型 <span class="muted" id="overviewModelCount"></span></h2>
      <div id="overviewModels" class="scroll"></div>
      <div class="row" style="margin-top:10px">
        <button id="overviewRefreshModels">同步模型</button>
      </div>
    </div>
  </div>
  <div class="grid" style="margin-top:14px">
    <div class="card">
      <h2>公告</h2>
      <div id="overviewAnnouncements" class="scroll"></div>
    </div>
    <div class="card">
      <h2>连通性测试</h2>
      <div class="muted">发送一条极小的测试请求，验证 User-Agent、密钥与协议选择。</div>
      <label>测试模型</label>
      <input id="testModel" placeholder="留空使用列表中的第一个模型">
      <div class="row" style="margin-top:10px"><button id="testBtn">测试</button></div>
      <div id="testResult" style="margin-top:10px"></div>
    </div>
  </div>
</section>

<section class="page" id="page-dashboard">
  <div class="card">
    <div class="row" style="justify-content:space-between">
      <h2>数据看板</h2>
      <div class="row">
        <select id="dashboardDays" style="width:auto">
          <option value="1">最近 1 天</option>
          <option value="7" selected>最近 7 天</option>
          <option value="30">最近 30 天</option>
          <option value="90">最近 90 天</option>
        </select>
        <button id="dashboardRefresh" class="small">刷新</button>
      </div>
    </div>
    <div id="dashboardMetrics" class="metrics" style="margin:4px 0 16px"></div>
    <div class="charts">
      <div>
        <div class="chart-title">消费趋势（美元）</div>
        <div class="bars" id="chartCost"></div>
      </div>
      <div>
        <div class="chart-title">请求次数</div>
        <div class="bars" id="chartCount"></div>
      </div>
    </div>
    <h3>按模型统计</h3>
    <div class="scroll"><table><thead><tr><th>模型</th><th>请求</th><th>Tokens</th><th>消费</th></tr></thead><tbody id="dashboardByModel"></tbody></table></div>
  </div>
</section>

<section class="page" id="page-models">
  <div class="card">
    <div class="row" style="justify-content:space-between">
      <h2>模型列表 <span class="muted" id="modelsCount"></span></h2>
      <button id="modelsRefresh" class="small primary">同步模型</button>
    </div>
    <div class="muted" id="modelsMeta"></div>
    <div class="scroll" style="margin-top:8px">
      <table><thead><tr><th>模型</th><th>协议</th><th>上下文</th><th>输入</th></tr></thead><tbody id="modelsRows"></tbody></table>
    </div>
  </div>
  <div class="card" style="margin-top:14px">
    <h2>API 令牌</h2>
    <div class="muted">来自 AgentRouter 控制台的令牌列表；插件使用名称匹配或自动创建的令牌作为推理密钥。</div>
    <div id="apiKeysBox" class="scroll" style="margin-top:8px"></div>
  </div>
</section>

<section class="page" id="page-logs">
  <div class="card">
    <div class="row" style="justify-content:space-between;align-items:flex-end">
      <h2>使用日志</h2>
      <div class="row">
        <select id="logType" style="width:auto">
          <option value="-1">全部类型</option>
          <option value="2">消费</option>
          <option value="5">错误</option>
          <option value="4">签到</option>
        </select>
        <select id="logRange" style="width:auto">
          <option value="1">最近 24 小时</option>
          <option value="7" selected>最近 7 天</option>
          <option value="30">最近 30 天</option>
        </select>
        <button id="logsRefresh" class="small">查询</button>
      </div>
    </div>
    <div id="logsBox" class="scroll" style="margin-top:10px"></div>
    <div class="row" style="justify-content:space-between;margin-top:10px">
      <span class="muted" id="logsMeta"></span>
      <div class="row"><button id="logsPrev" class="small">上一页</button><button id="logsNext" class="small">下一页</button></div>
    </div>
  </div>
</section>

<section class="page" id="page-account">
  <div class="grid two">
    <div class="card">
      <h2>账号授权</h2>
      <div id="accountStateBox"></div>
      <div class="split" style="margin-top:10px">
        <div>
          <h3>账号密码登录</h3>
          <label>账号（用户名 / 邮箱）</label><input id="loginUsername" autocomplete="username" placeholder="AgentRouter 登录账号">
          <label>密码</label><input id="loginPassword" type="password" autocomplete="current-password" placeholder="不会被回显">
          <div class="row" style="margin-top:10px"><button id="loginBtn" class="primary">登录并授权</button></div>
        </div>
        <div>
          <h3>系统令牌 / 会话</h3>
          <label>访问令牌</label><input id="loginToken" type="password" placeholder="个人设置 → 系统令牌">
          <label>用户 ID</label><input id="loginUserID" placeholder="例如 123456">
          <div class="row" style="margin-top:10px"><button id="tokenBtn">保存令牌</button></div>
          <label>或粘贴浏览器 Cookie</label><input id="loginSession" type="password" placeholder="session=...">
          <div class="row" style="margin-top:10px"><button id="sessionBtn">保存会话</button></div>
          <div class="hint">GitHub / LinuxDO 登录的账号没有密码：登录后从浏览器开发者工具复制 session Cookie 的值即可。登录遇到 Turnstile 验证码时，系统令牌最稳妥。</div>
        </div>
      </div>
      <div id="loginResult" style="margin-top:12px"></div>
    </div>
    <div class="card">
      <h2>签到</h2>
      <div id="checkinBox"></div>
      <div class="row" style="margin-top:10px"><button id="accountCheckinBtn" class="primary">退出并重新登录签到</button></div>
      <div class="hint">AgentRouter 在登录时发放每日奖励，因此签到流程是「退出 → 重新登录」。</div>
    </div>
  </div>
</section>

<section class="page" id="page-settings">
  <div class="card">
    <h2>插件配置</h2>
    <div class="muted">这里的修改会保存到插件状态文件，优先级高于 config.yaml。</div>
    <div class="grid" style="margin-top:12px">
      <div>
        <label>API 地址</label><input id="cfgBaseURL" placeholder="https://ps.air-outer.com">
        <label>User-Agent</label><input id="cfgUserAgent" placeholder="claude-cli/2.1.161 (external, cli)">
        <label>代理（仅控制面请求）</label><input id="cfgProxyURL" placeholder="http://127.0.0.1:7890">
      </div>
      <div>
        <label>API Key（推理鉴权）</label><input id="cfgAPIKey" type="password" placeholder="留空保持不变">
        <label>签到账号</label><input id="cfgUsername" placeholder="用于每日签到的账号">
        <label>签到密码</label><input id="cfgPassword" type="password" placeholder="留空保持不变">
      </div>
    </div>
    <div class="row" style="margin-top:14px">
      <button id="settingsSave" class="primary">保存配置</button>
      <button id="settingsReload">重新读取</button>
    </div>
    <div id="settingsMeta" class="muted" style="margin-top:12px"></div>
  </div>
</section>

<div class="toast" id="toast"></div>

<script>
const API = {
  models: '/v0/management/agentrouter/models',
  settings: '/v0/management/agentrouter/settings',
  refreshModels: '/v0/management/agentrouter/refresh-models',
  account: '/v0/management/agentrouter/account',
  checkin: '/v0/management/agentrouter/checkin',
  checkinStatus: '/v0/management/agentrouter/checkin-status',
  test: '/v0/management/agentrouter/test',
  login: '/v0/management/agentrouter/login',
  token: '/v0/management/agentrouter/login-token',
  logs: '/v0/management/agentrouter/logs',
  dashboard: '/v0/management/agentrouter/dashboard',
  apiKeys: '/v0/management/agentrouter/api-keys',
  announcements: '/v0/management/agentrouter/announcements'
};
const state = { models: [], account: null, dashboard: null, logsPage: 0, logsTotal: 0, settings: null };
const $ = (id) => document.getElementById(id);
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
  try { payload = text ? JSON.parse(text) : null; } catch (e) { payload = { success: false, message: text }; }
  if (!response.ok) throw new Error((payload && payload.message) || ('HTTP ' + response.status));
  return payload || {};
}
function esc(value) {
  return String(value == null ? '' : value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}
function fmtUSD(value) { return '$' + Number(value || 0).toFixed(value >= 100 ? 0 : 2); }
function fmtNum(value) {
  const number = Number(value || 0);
  if (number >= 1e6) return (number / 1e6).toFixed(2) + 'M';
  if (number >= 1e3) return (number / 1e3).toFixed(1) + 'k';
  return String(number);
}
let toastTimer;
function toast(message, isError) {
  const el = $('toast');
  el.textContent = message;
  el.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast'; }, 3200);
}
function setBusy(button, busy) { if (button) button.disabled = busy; }

document.querySelectorAll('.tabs button').forEach(button => {
  button.addEventListener('click', () => {
    document.querySelectorAll('.tabs button').forEach(item => item.classList.toggle('active', item === button));
    document.querySelectorAll('.page').forEach(page => page.classList.toggle('active', page.id === 'page-' + button.dataset.tab));
    if (button.dataset.tab === 'dashboard') loadDashboard();
    if (button.dataset.tab === 'logs') loadLogs(0);
    if (button.dataset.tab === 'models') { loadModels(); loadAPIKeys(); }
    if (button.dataset.tab === 'account') { loadCheckinStatus(); }
  });
});

async function loadSettings() {
  const payload = await api(API.settings);
  state.settings = payload;
  $('baseURL').textContent = payload.base_url || '';
  $('cfgBaseURL').value = payload.base_url || '';
  $('cfgUserAgent').value = payload.user_agent || '';
  $('cfgProxyURL').value = payload.proxy_url || '';
  $('cfgUsername').value = payload.username || '';
  $('settingsMeta').innerHTML = 'API Key：' + (payload.has_api_key ? '<span class="ok">已配置 ' + esc(payload.api_key_masked || '') + '</span>' : '<span class="err">未配置</span>')
    + ' · 账号：' + (payload.has_account ? '<span class="ok">已授权</span>' : '<span class="err">未授权</span>')
    + ' · 数据目录：<code>' + esc(payload.data_dir || '') + '</code>';
  const dot = $('healthDot'), text = $('healthText');
  dot.className = 'status-dot' + (payload.has_api_key && payload.has_account ? ' on' : '');
  text.textContent = payload.has_api_key ? (payload.has_account ? '推理与账号均已就绪' : '推理就绪，账号未授权') : 'API Key 未配置';
  return payload;
}

async function loadModels() {
  const payload = await api(API.models);
  state.models = payload.models || [];
  $('modelsCount').textContent = '（' + state.models.length + '）';
  $('overviewModelCount').textContent = state.models.length ? state.models.length + ' 个可用模型' : '';
  $('modelsMeta').textContent = payload.last_sync ? ('上次同步：' + payload.last_sync + (payload.last_error ? ' · ' + payload.last_error : '')) : '尚未从 AgentRouter 同步模型列表';
  $('modelsRows').innerHTML = state.models.map(model =>
    '<tr><td>' + esc(model.name) + '<div class="muted">' + esc(model.display_name !== model.name ? model.display_name : (model.client_name || '')) + '</div></td>'
    + '<td>' + (model.protocols || [model.protocol]).map(protocol => '<span class="pill">' + esc(protocol) + '</span>').join('') + '</td>'
    + '<td>' + (model.context_length ? fmtNum(model.context_length) : '-') + '</td>'
    + '<td>' + (model.input_modalities || []).map(item => '<span class="pill' + (item === 'image' ? ' ok' : '') + '">' + esc(item) + '</span>').join('') + '</td></tr>'
  ).join('') || '<tr><td colspan="4" class="empty">暂无模型</td></tr>';
  $('overviewModels').innerHTML = state.models.length
    ? '<table><tbody>' + state.models.slice(0, 8).map(model =>
        '<tr><td>' + esc(model.name) + '</td><td>' + (model.protocols || [model.protocol]).map(protocol => '<span class="pill">' + esc(protocol) + '</span>').join('') + '</td><td class="muted">' + (model.context_length ? fmtNum(model.context_length) : '-') + '</td></tr>'
      ).join('') + '</tbody></table>'
    : '<div class="empty">尚未同步模型</div>';
  return payload;
}

function renderAccount(info) {
  state.account = info;
  const metrics = $('overviewMetrics');
  const detail = $('overviewAccount');
  if (!metrics) return;
  if (!info || info.error) {
    metrics.innerHTML = '';
    detail.innerHTML = '<div class="banner ' + (info && info.error ? 'error' : '') + '">' + esc((info && info.error) || '暂无账号数据') + '</div>';
    const box = $('accountStateBox');
    if (box) box.innerHTML = '';
    return;
  }
  if (info.success) {
    metrics.innerHTML = [
      ['当前余额', fmtUSD(info.quota_usd), info.group ? ('分组 ' + info.group) : ''],
      ['历史消耗', fmtUSD(info.used_usd), ''],
      ['请求次数', String(info.request_count || 0), ''],
      ['今日签到', info.checked_in ? '<span class="ok">已签到</span>' : '<span class="warn">未签到</span>', info.checked_in_at || '']
    ].map(([label, value, sub]) => '<div class="metric"><div class="label">' + label + '</div><div class="value">' + value + '</div><div class="sub">' + esc(sub) + '</div></div>').join('');
  } else {
    metrics.innerHTML = '';
  }
  detail.innerHTML = [
    ['账号', esc(info.display_name || info.username || '-')],
    ['用户 ID', esc(info.user_id || '-')],
    ['分组', esc(info.group || '-')],
    ['可用模型', esc((info.models || []).join(', ') || '-')],
    ['推理密钥', esc(info.api_key_masked || '-')],
    ['授权状态', info.authorized ? '<span class="ok">已授权</span>' : '<span class="err">未授权</span>']
  ].map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('');
  if (info.message) detail.innerHTML += '<div class="banner warn" style="margin-top:10px">' + esc(info.message) + '</div>';
  const accountState = $('accountStateBox');
  if (accountState) {
    accountState.innerHTML = [
      ['账号', esc(info.display_name || info.username || '-')],
      ['用户 ID', esc(info.user_id || '-')],
      ['分组', esc(info.group || '-')],
      ['余额', fmtUSD(info.quota_usd)],
      ['授权', info.authorized ? '<span class="ok">是</span>' : '<span class="err">否</span>']
    ].map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('')
      + (info.message ? '<div class="banner warn" style="margin-top:10px">' + esc(info.message) + '</div>' : '');
  }
  $('overviewBanner').innerHTML = info.authorized ? '' : '<div class="banner warn" style="margin-bottom:14px">账号尚未授权。前往「账号与授权」登录，即可查看余额、日志、模型状态与签到。</div>';
}

async function queryAccount() {
  try {
    const payload = await api(API.account);
    renderAccount(payload.account);
  } catch (error) {
    renderAccount({ error: error.message });
  }
}

async function loadCheckinStatus() {
  try {
    const payload = await api(API.checkinStatus);
    const rows = [];
    rows.push(['今日签到', payload.checked_in ? '<span class="ok">已签到</span>' : '<span class="warn">未签到</span>' + (payload.checked_in_at ? ' · ' + esc(payload.checked_in_at) : '')]);
    if (payload.last_checkin) {
      rows.push(['最近签到', esc(payload.last_checkin.checked_in_at || '') + (payload.last_checkin.quota_awarded ? ' · 奖励 ' + fmtUSD(payload.last_checkin.quota_awarded) : '')]);
    }
    if (payload.server_error) rows.push(['服务端', '<span class="warn">' + esc(payload.server_error) + '</span>']);
    const html = rows.map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('');
    $('checkinStatusBox').innerHTML = html;
    $('checkinBox').innerHTML = html + (payload.last_checkin ? '<div class="stat"><span class="muted">消息</span><span>' + esc(payload.last_checkin.message) + '</span></div>' : '');
  } catch (error) {
    $('checkinStatusBox').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
    $('checkinBox').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  }
}

async function runCheckin(button) {
  setBusy(button, true);
  try {
    const payload = await api(API.checkin, { method: 'POST', body: JSON.stringify({}) });
    const result = payload.result || {};
    toast(result.message || '签到成功');
    $('checkinBox').innerHTML = '<div class="banner"><strong>' + esc(result.message || '签到成功') + '</strong>'
      + (result.quota_awarded ? '<div class="muted">奖励 ' + fmtUSD(result.quota_awarded / 500000) + '</div>' : '')
      + '</div>';
    await Promise.all([queryAccount(), loadCheckinStatus()]);
  } catch (error) {
    toast('签到失败：' + error.message, true);
    $('checkinBox').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  } finally { setBusy(button, false); }
}

async function refreshModels(button) {
  setBusy(button, true);
  try {
    const payload = await api(API.refreshModels, { method: 'POST', body: JSON.stringify({}) });
    toast('已同步 ' + payload.count + ' 个模型');
    await loadModels();
  } catch (error) { toast('同步失败：' + error.message, true); }
  finally { setBusy(button, false); }
}

async function runTest() {
  const button = $('testBtn');
  setBusy(button, true);
  $('testResult').textContent = '测试中…';
  try {
    const payload = await api(API.test, { method: 'POST', body: JSON.stringify({ model: $('testModel').value.trim() }) });
    $('testResult').innerHTML = '<div class="ok">' + esc(payload.model) + ' · ' + esc(payload.protocol) + ' · ' + esc(payload.latency_ms) + ' ms</div><pre>' + esc(payload.response || '') + '</pre>';
  } catch (error) {
    $('testResult').innerHTML = '<div class="err">' + esc(error.message) + '</div>';
  } finally { setBusy(button, false); }
}

function renderBars(container, values, format) {
  if (!container) return;
  const max = Math.max(1, ...values.map(item => item.value));
  container.innerHTML = values.map(item =>
    '<div class="bar" style="height:' + Math.max(2, Math.round(item.value / max * 100)) + '%"><span>' + esc(item.label) + '：' + format(item.value) + '</span></div>'
  ).join('') || '<div class="empty">暂无数据</div>';
}

async function loadDashboard() {
  const days = $('dashboardDays') ? $('dashboardDays').value : '7';
  try {
    const payload = await api(API.dashboard + '?days=' + encodeURIComponent(days));
    state.dashboard = payload;
    if (!payload.success) {
      $('dashboardMetrics').innerHTML = '<div class="banner warn">' + esc(payload.message || '账号尚未授权') + '</div>';
      $('chartCost').innerHTML = '';
      $('chartCount').innerHTML = '';
      $('dashboardByModel').innerHTML = '';
      return;
    }
    $('dashboardMetrics').innerHTML = [
      ['当前余额', fmtUSD(payload.quota_usd), payload.group ? '分组 ' + payload.group : ''],
      ['区间消费', fmtUSD(payload.total_usd), '最近 ' + payload.days + ' 天'],
      ['请求次数', fmtNum(payload.total_count), ''],
      ['Tokens', fmtNum(payload.total_tokens), '']
    ].map(([label, value, sub]) => '<div class="metric"><div class="label">' + label + '</div><div class="value">' + value + '</div><div class="sub">' + esc(sub) + '</div></div>').join('');
    const timeline = payload.timeline || [];
    renderBars($('chartCost'), timeline.map(item => ({ label: item.time, value: item.quota_usd })), fmtUSD);
    renderBars($('chartCount'), timeline.map(item => ({ label: item.time, value: item.count })), value => fmtNum(value));
    $('dashboardByModel').innerHTML = (payload.by_model || []).sort((a, b) => b.quota_usd - a.quota_usd).map(item =>
      '<tr><td>' + esc(item.model) + '</td><td>' + fmtNum(item.count) + '</td><td>' + fmtNum(item.tokens) + '</td><td>' + fmtUSD(item.quota_usd) + '</td></tr>'
    ).join('') || '<tr><td colspan="4" class="empty">暂无数据</td></tr>';
  } catch (error) {
    $('dashboardMetrics').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  }
}

async function loadLogs(page) {
  const days = Number($('logRange').value || 7);
  const end = Math.floor(Date.now() / 1000);
  const start = end - days * 86400;
  const type = $('logType').value;
  try {
    const payload = await api(API.logs + '?page=' + page + '&page_size=20&type=' + encodeURIComponent(type) + '&start=' + start + '&end=' + end);
    if (!payload.success) {
      $('logsBox').innerHTML = '<div class="banner warn">' + esc(payload.message || '账号尚未授权') + '</div>';
      $('logsMeta').textContent = '';
      return;
    }
    state.logsPage = payload.page ? payload.page - 1 : page;
    state.logsTotal = payload.total || 0;
    $('logsMeta').textContent = '共 ' + state.logsTotal + ' 条 · 第 ' + (state.logsPage + 1) + ' 页';
    $('logsBox').innerHTML = (payload.items || []).length ? '<table><thead><tr><th>时间</th><th>模型</th><th>类型</th><th>Tokens</th><th>消费</th><th>详情</th></tr></thead><tbody>'
      + payload.items.map(item =>
        '<tr><td class="muted">' + esc(item.created_at_text) + '</td><td>' + esc(item.model_name || '-') + '</td>'
        + '<td><span class="pill ' + (item.type === 5 ? 'err' : item.type === 4 ? 'ok' : '') + '">' + esc(item.type_label) + '</span></td>'
        + '<td class="muted">' + fmtNum((item.prompt_tokens || 0) + (item.completion_tokens || 0)) + '</td>'
        + '<td>' + fmtUSD(item.quota_usd) + '</td><td class="muted">' + esc(item.content || '') + '</td></tr>'
      ).join('') + '</tbody></table>' : '<div class="empty">没有匹配的日志</div>';
  } catch (error) {
    $('logsBox').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  }
}

async function loadAPIKeys() {
  try {
    const payload = await api(API.apiKeys);
    if (!payload.success) {
      $('apiKeysBox').innerHTML = '<div class="banner warn">' + esc(payload.message || '账号尚未授权') + '</div>';
      return;
    }
    $('apiKeysBox').innerHTML = '<table><thead><tr><th>名称</th><th>密钥</th><th>状态</th><th>用量</th></tr></thead><tbody>'
      + (payload.items || []).map(token =>
        '<tr><td>' + esc(token.name) + (token.in_use ? ' <span class="pill ok">使用中</span>' : '') + '</td>'
        + '<td><code>' + esc(token.key_masked) + '</code></td>'
        + '<td>' + (token.status === 1 ? '<span class="pill ok">启用</span>' : '<span class="pill err">停用</span>') + '</td>'
        + '<td>' + fmtUSD(token.used_usd) + '</td></tr>'
      ).join('') + '</tbody></table>';
  } catch (error) {
    $('apiKeysBox').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  }
}

async function loadAnnouncements() {
  try {
    const payload = await api(API.announcements);
    if (!payload.success) { $('overviewAnnouncements').innerHTML = '<div class="empty">' + esc(payload.message || '公告不可用') + '</div>'; return; }
    $('overviewAnnouncements').innerHTML = (payload.announcements || []).slice(0, 6).map(item =>
      '<div class="stat" style="display:block"><div class="muted">' + esc(item.publish_date) + '</div><div style="white-space:pre-wrap;font-size:13px;margin-top:4px">' + esc(item.content.length > 400 ? item.content.slice(0, 400) + '…' : item.content) + '</div></div>'
    ).join('') || '<div class="empty">暂无公告</div>';
  } catch (error) {
    $('overviewAnnouncements').innerHTML = '<div class="empty">' + esc(error.message) + '</div>';
  }
}

async function doLogin() {
  const button = $('loginBtn');
  setBusy(button, true);
  $('loginResult').textContent = '正在登录 AgentRouter…';
  try {
    const payload = await api(API.login, { method: 'POST', body: JSON.stringify({ username: $('loginUsername').value.trim(), password: $('loginPassword').value }) });
    $('loginResult').innerHTML = '<div class="banner"><strong>' + esc(payload.message || '登录成功') + '</strong>'
      + (payload.api_key_name ? '<div class="muted">推理密钥：' + esc(payload.api_key_name) + '</div>' : '')
      + '</div>';
    $('loginPassword').value = '';
    toast('授权成功');
    await Promise.all([loadSettings(), queryAccount(), loadModels(), loadCheckinStatus()]);
  } catch (error) {
    $('loginResult').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  } finally { setBusy(button, false); }
}

async function saveSession() {
  const button = $('sessionBtn');
  setBusy(button, true);
  try {
    const payload = await api(API.token, { method: 'POST', body: JSON.stringify({ session: $('loginSession').value.trim(), user_id: $('loginUserID').value.trim() }) });
    $('loginResult').innerHTML = '<div class="banner"><strong>' + esc(payload.message || '会话已保存') + '</strong></div>';
    $('loginSession').value = '';
    toast('浏览器会话已保存');
    await Promise.all([loadSettings(), queryAccount()]);
  } catch (error) {
    $('loginResult').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  } finally { setBusy(button, false); }
}

async function saveToken() {
  const button = $('tokenBtn');
  setBusy(button, true);
  try {
    const payload = await api(API.token, { method: 'POST', body: JSON.stringify({ token: $('loginToken').value.trim(), user_id: $('loginUserID').value.trim() }) });
    $('loginResult').innerHTML = '<div class="banner"><strong>' + esc(payload.message || '令牌已保存') + '</strong></div>';
    $('loginToken').value = '';
    toast('访问令牌已保存');
    await Promise.all([loadSettings(), queryAccount()]);
  } catch (error) {
    $('loginResult').innerHTML = '<div class="banner error">' + esc(error.message) + '</div>';
  } finally { setBusy(button, false); }
}

async function saveSettings() {
  const button = $('settingsSave');
  setBusy(button, true);
  const body = {
    base_url: $('cfgBaseURL').value.trim(),
    user_agent: $('cfgUserAgent').value.trim(),
    proxy_url: $('cfgProxyURL').value.trim(),
    username: $('cfgUsername').value.trim()
  };
  if ($('cfgAPIKey').value.trim()) body.api_key = $('cfgAPIKey').value.trim();
  if ($('cfgPassword').value) body.password = $('cfgPassword').value;
  try {
    const payload = await api(API.settings, { method: 'POST', body: JSON.stringify(body) });
    $('cfgAPIKey').value = '';
    $('cfgPassword').value = '';
    toast(payload.message || '配置已保存');
    await Promise.all([loadSettings(), loadModels()]);
  } catch (error) { toast('保存失败：' + error.message, true); }
  finally { setBusy(button, false); }
}

async function loadAll() {
  try {
    await Promise.all([loadSettings(), loadModels(), queryAccount(), loadAnnouncements()]);
    loadCheckinStatus();
  } catch (error) { toast(error.message, true); }
}

$('refreshAll').addEventListener('click', () => loadAll());
$('accountBtn').addEventListener('click', () => queryAccount());
$('checkinStatusBtn').addEventListener('click', () => loadCheckinStatus());
$('checkinBtn').addEventListener('click', () => runCheckin($('checkinBtn')));
$('accountCheckinBtn').addEventListener('click', () => runCheckin($('accountCheckinBtn')));
$('overviewRefreshModels').addEventListener('click', () => refreshModels($('overviewRefreshModels')));
$('modelsRefresh').addEventListener('click', () => refreshModels($('modelsRefresh')));
$('testBtn').addEventListener('click', runTest);
$('dashboardRefresh').addEventListener('click', () => loadDashboard());
$('dashboardDays').addEventListener('change', () => loadDashboard());
$('logsRefresh').addEventListener('click', () => loadLogs(0));
$('logsPrev').addEventListener('click', () => loadLogs(Math.max(0, state.logsPage - 1)));
$('logsNext').addEventListener('click', () => loadLogs(state.logsPage + 1));
$('loginBtn').addEventListener('click', doLogin);
$('tokenBtn').addEventListener('click', saveToken);
$('sessionBtn').addEventListener('click', saveSession);
$('settingsSave').addEventListener('click', saveSettings);
$('settingsReload').addEventListener('click', () => loadSettings());

loadAll();
setInterval(() => { if (document.querySelector('#page-dashboard.active')) loadDashboard(); }, 60000);
</script>
</body>
</html>
`
