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
<meta name="color-scheme" content="light dark">
<title>AgentRouter</title>
<style>
{{STYLE}}
</style>
</head>
<body>
<div class="app">

<header class="header">
  <div class="brand">
    <div class="brand-mark" aria-hidden="true">AR</div>
    <div class="brand-text">
      <h1>AgentRouter</h1>
      <div class="meta">
        <span class="health"><span id="healthDot" class="status-dot"></span><span id="healthText">加载中…</span></span>
        <span class="meta-sep" aria-hidden="true"></span>
        <span>网关 <span id="baseURL" class="mono">{{BASE_URL}}</span></span>
        <span class="meta-sep" aria-hidden="true"></span>
        <span>插件 v{{PLUGIN_VERSION}}</span>
      </div>
    </div>
  </div>
  <div class="row header-actions">
    <button id="refreshAll" class="primary">刷新全部</button>
    <button id="checkinBtn">立即签到</button>
  </div>
</header>

<nav class="tabs" role="tablist" aria-label="控制台导航">
  <button type="button" role="tab" data-tab="overview" class="active">概览</button>
  <button type="button" role="tab" data-tab="dashboard">数据看板</button>
  <button type="button" role="tab" data-tab="models">模型</button>
  <button type="button" role="tab" data-tab="logs">使用日志</button>
  <button type="button" role="tab" data-tab="account">账号与授权</button>
  <button type="button" role="tab" data-tab="settings">设置</button>
</nav>

<section class="page active" id="page-overview">
  <div id="overviewBanner"></div>
  <div class="grid overview">
    <div class="card">
      <div class="card-head">
        <h2>账号</h2>
        <div class="row">
          <button id="accountBtn" class="small">查询账号</button>
          <button id="checkinStatusBtn" class="small ghost">签到状态</button>
        </div>
      </div>
      <div class="metrics" id="overviewMetrics"></div>
      <div class="stack" id="overviewAccount"></div>
      <div class="stack" id="checkinStatusBox"></div>
    </div>
    <div class="card">
      <div class="card-head">
        <h2>模型 <span class="muted" id="overviewModelCount"></span></h2>
        <button id="overviewRefreshModels" class="small">同步模型</button>
      </div>
      <div id="overviewModels" class="scroll"><div class="skeleton-stack" aria-hidden="true"><div class="skeleton skeleton-line medium"></div><div class="skeleton skeleton-line"></div><div class="skeleton skeleton-line short"></div><div class="skeleton skeleton-line medium"></div></div></div>
    </div>
  </div>
  <div class="grid overview-lower">
    <div class="card">
      <div class="card-head">
        <h2>公告</h2>
        <span class="card-note">来自 AgentRouter 控制台</span>
      </div>
      <div id="overviewAnnouncements" class="scroll"><div class="skeleton-stack" aria-hidden="true"><div class="skeleton skeleton-line tall"></div><div class="skeleton skeleton-line"></div><div class="skeleton skeleton-line short"></div></div></div>
    </div>
    <div class="card">
      <div class="card-head">
        <h2>连通性测试</h2>
      </div>
      <div class="card-desc">发送一条极小的测试请求，验证 User-Agent、密钥与协议选择。</div>
      <label for="testModel">测试模型</label>
      <input id="testModel" placeholder="留空使用列表中的第一个模型">
      <div class="field-hint">可选：填写模型名称以指定测试目标。</div>
      <div class="row" style="margin-top:12px"><button id="testBtn">测试</button></div>
      <div id="testResult" class="stack"></div>
    </div>
  </div>
</section>

<section class="page" id="page-dashboard">
  <div class="card">
    <div class="card-head">
      <h2>数据看板</h2>
      <div class="row">
        <select id="dashboardDays" class="auto-width">
          <option value="1">最近 1 天</option>
          <option value="7" selected>最近 7 天</option>
          <option value="30">最近 30 天</option>
          <option value="90">最近 90 天</option>
        </select>
        <button id="dashboardRefresh" class="small">刷新</button>
      </div>
    </div>
    <div id="dashboardMetrics" class="metrics metrics-block"></div>
    <div class="charts">
      <div class="chart-panel">
        <div class="chart-title">消费趋势（美元）</div>
        <div class="bars" id="chartCost"></div>
        <div class="legend"><span><i></i>每日消费</span><span class="legend-hint">悬停查看具体数值</span></div>
      </div>
      <div class="chart-panel">
        <div class="chart-title">请求次数</div>
        <div class="bars" id="chartCount"></div>
        <div class="legend"><span><i></i>每日请求</span><span class="legend-hint">悬停查看具体数值</span></div>
      </div>
    </div>
    <h3>按模型统计</h3>
    <div class="scroll"><table><thead><tr><th>模型</th><th>请求</th><th>Tokens</th><th>消费</th></tr></thead><tbody id="dashboardByModel"></tbody></table></div>
  </div>
</section>

<section class="page" id="page-models">
  <div class="card">
    <div class="card-head">
      <h2>模型列表 <span class="muted" id="modelsCount"></span></h2>
      <button id="modelsRefresh" class="small primary">同步模型</button>
    </div>
    <div class="muted" id="modelsMeta"></div>
    <div class="scroll" style="margin-top:12px">
      <table><thead><tr><th>模型</th><th>协议</th><th>上下文</th><th>输入</th></tr></thead><tbody id="modelsRows"></tbody></table>
    </div>
  </div>
  <div class="card" style="margin-top:16px">
    <div class="card-head">
      <h2>API 令牌</h2>
    </div>
    <div class="card-desc">来自 AgentRouter 控制台的令牌列表；插件使用名称匹配或自动创建的令牌作为推理密钥。</div>
    <div id="apiKeysBox" class="scroll" style="margin-top:12px"></div>
  </div>
</section>

<section class="page" id="page-logs">
  <div class="card">
    <div class="card-head">
      <h2>使用日志</h2>
      <div class="row">
        <select id="logType" class="auto-width">
          <option value="-1">全部类型</option>
          <option value="2">消费</option>
          <option value="5">错误</option>
          <option value="4">签到</option>
        </select>
        <select id="logRange" class="auto-width">
          <option value="1">最近 24 小时</option>
          <option value="7" selected>最近 7 天</option>
          <option value="30">最近 30 天</option>
        </select>
        <button id="logsRefresh" class="small">查询</button>
      </div>
    </div>
    <div id="logsBox" class="scroll"></div>
    <div class="row log-footer">
      <span class="muted" id="logsMeta"></span>
      <div class="row"><button id="logsPrev" class="small">上一页</button><button id="logsNext" class="small">下一页</button></div>
    </div>
  </div>
</section>

<section class="page" id="page-account">
  <div class="grid account">
    <div class="card">
      <div class="card-head">
        <h2>账号授权</h2>
      </div>
      <div id="accountStateBox"></div>
      <div class="split" style="margin-top:16px">
        <div>
          <h3>账号密码登录</h3>
          <label for="loginUsername">账号（用户名 / 邮箱）</label><input id="loginUsername" autocomplete="username" placeholder="AgentRouter 登录账号">
          <label for="loginPassword">密码</label><input id="loginPassword" type="password" autocomplete="current-password" placeholder="不会被回显">
          <div class="row" style="margin-top:12px"><button id="loginBtn" class="primary">登录并授权</button></div>
        </div>
        <div>
          <h3>系统令牌 / 会话</h3>
          <label for="loginToken">访问令牌</label><input id="loginToken" type="password" placeholder="个人设置 → 系统令牌">
          <label for="loginUserID">用户 ID</label><input id="loginUserID" placeholder="例如 123456">
          <div class="row" style="margin-top:12px"><button id="tokenBtn">保存令牌</button></div>
          <label for="loginSession">或粘贴浏览器 Cookie</label><input id="loginSession" type="password" placeholder="session=...">
          <div class="row" style="margin-top:12px"><button id="sessionBtn">保存会话</button></div>
          <div class="hint">GitHub / LinuxDO 登录的账号没有密码：登录后从浏览器开发者工具复制 session Cookie 的值即可。登录遇到 Turnstile 验证码时，系统令牌最稳妥。</div>
        </div>
      </div>
      <div id="loginResult" class="stack"></div>
    </div>
    <div class="card">
      <div class="card-head">
        <h2>签到</h2>
      </div>
      <div id="checkinBox"></div>
      <div class="row" style="margin-top:12px"><button id="accountCheckinBtn" class="primary">退出并重新登录签到</button></div>
      <div class="hint">AgentRouter 在登录时发放每日奖励，因此签到流程是「退出 → 重新登录」。</div>
    </div>
  </div>
</section>

<section class="page" id="page-settings">
  <div class="card">
    <div class="card-head">
      <h2>插件配置</h2>
    </div>
    <div class="card-desc">这里的修改会保存到插件状态文件，优先级高于 config.yaml。</div>
    <div class="grid" style="margin-top:16px">
      <div>
        <label for="cfgBaseURL">API 地址</label><input id="cfgBaseURL" placeholder="https://ps.air-outer.com">
        <label for="cfgUserAgent">User-Agent</label><input id="cfgUserAgent" placeholder="claude-cli/2.1.161 (external, cli)">
        <label for="cfgProxyURL">代理（仅控制面请求）</label><input id="cfgProxyURL" placeholder="http://127.0.0.1:7890">
        <div class="field-hint">留空则直连，不经过代理。</div>
      </div>
      <div>
        <label for="cfgAPIKey">API Key（推理鉴权）</label><input id="cfgAPIKey" type="password" placeholder="留空保持不变">
        <label for="cfgUsername">签到账号</label><input id="cfgUsername" placeholder="用于每日签到的账号">
        <label for="cfgPassword">签到密码</label><input id="cfgPassword" type="password" placeholder="留空保持不变">
      </div>
    </div>
    <div class="row" style="margin-top:16px">
      <button id="settingsSave" class="primary">保存配置</button>
      <button id="settingsReload" class="ghost">重新读取</button>
    </div>
    <div id="settingsMeta" class="muted" style="margin-top:16px"></div>
  </div>
</section>

</div>
<div class="toast" id="toast" role="status" aria-live="polite"></div>

<script>
{{SCRIPT}}
</script>
</body>
</html>
`
