package main

// dashboardJS holds the console client script. It must keep using the
// element ids and data-tab values rendered by dashboard_template.go.
const dashboardJS = `
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
const CACHE_TTL = 90000;
const CACHE_PREFIX = 'agentrouter.console.';
const POLL_INTERVAL = 60000;
const state = {
  models: [], account: null, dashboard: null, logsPage: 0, logsTotal: 0, settings: null,
  activeTab: 'overview'
};
const $ = (id) => document.getElementById(id);
const inflight = new Map();
const viewControllers = new Map();
function managementKey() {
  try {
    const auth = JSON.parse(localStorage.getItem('cli-proxy-auth') || '{}');
    if (auth && auth.state && typeof auth.state.managementKey === 'string') return auth.state.managementKey;
  } catch (e) {}
  return '';
}
function apiError(message, kind, status) {
  const error = new Error(message);
  error.kind = kind || 'http';
  if (status) error.status = status;
  return error;
}
function isAbort(error) { return !!error && (error.name === 'AbortError' || error.kind === 'abort'); }
function isNetworkError(error) { return !!error && error.kind === 'network'; }
async function fetchPayload(path, options) {
  const key = managementKey();
  const headers = Object.assign({ 'Content-Type': 'application/json' }, (options && options.headers) || {});
  if (key) headers['Authorization'] = 'Bearer ' + key;
  let response;
  try {
    response = await fetch(path, Object.assign({}, options || {}, { headers }));
  } catch (error) {
    if (isAbort(error)) throw error;
    throw apiError('网络连接失败，请检查网关是否在线', 'network');
  }
  const text = await response.text();
  let payload = null;
  try { payload = text ? JSON.parse(text) : null; } catch (e) { payload = { success: false, message: text }; }
  if (!response.ok) {
    const kind = response.status === 401 || response.status === 403 ? 'auth' : 'http';
    throw apiError((payload && payload.message) || ('HTTP ' + response.status), kind, response.status);
  }
  return payload || {};
}
function api(path, options) {
  const init = Object.assign({}, options || {});
  const signal = init.signal || null;
  delete init.signal;
  const method = String(init.method || 'GET').toUpperCase();
  if (method !== 'GET') return fetchPayload(path, Object.assign({}, init, { signal }));
  const flightKey = method + ' ' + path;
  let flight = inflight.get(flightKey);
  if (!flight || flight.controller.signal.aborted) {
    const controller = new AbortController();
    flight = { controller: controller, waiters: 0 };
    flight.promise = fetchPayload(path, Object.assign({}, init, { signal: controller.signal })).finally(() => {
      if (inflight.get(flightKey) === flight) inflight.delete(flightKey);
    });
    flight.promise.catch(() => {});
    inflight.set(flightKey, flight);
  }
  flight.waiters += 1;
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (fn, value) => {
      if (settled) return;
      settled = true;
      flight.waiters -= 1;
      if (signal) signal.removeEventListener('abort', onAbort);
      fn(value);
    };
    const onAbort = () => {
      finish(reject, apiError('请求已取消', 'abort'));
      if (flight.waiters <= 0) flight.controller.abort();
    };
    if (signal) {
      if (signal.aborted) { onAbort(); return; }
      signal.addEventListener('abort', onAbort, { once: true });
    }
    flight.promise.then(value => finish(resolve, value), error => finish(reject, error));
  });
}
function esc(value) {
  return String(value == null ? '' : value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}
function fmtUSD(value) {
  const number = Number(value || 0);
  if (!isFinite(number)) return '$0.00';
  if (number >= 100) return '$' + number.toFixed(0);
  if (number >= 1) return '$' + number.toFixed(2);
  if (number >= 0.01) return '$' + number.toFixed(3);
  if (number <= 0) return '$0.00';
  return '$' + number.toFixed(4);
}
function fmtNum(value) {
  const number = Number(value || 0);
  if (number >= 1e6) return (number / 1e6).toFixed(2) + 'M';
  if (number >= 1e3) return (number / 1e3).toFixed(1) + 'k';
  return String(number);
}
function isUnauthorized(error) {
  return !!error && (error.kind === 'auth' || /未授权|not\s*authori[sz]ed|unauthorized|\b40[13]\b/i.test(error.message || ''));
}
function describeError(error) {
  if (!error) return '请求失败';
  if (isNetworkError(error)) return '网络连接失败，请确认网关在线后重试。';
  if (isUnauthorized(error)) return '未授权：' + (error.message || '请先在「账号与授权」完成登录。');
  return error.message || '请求失败';
}
let toastTimer = null;
function toast(message, isError) {
  const el = $('toast');
  if (!el) return;
  el.className = 'toast' + (isError ? ' err' : '');
  el.textContent = message;
  void el.offsetWidth;
  el.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast' + (isError ? ' err' : ''); }, 3200);
}
function setBusy(button, busy, label) {
  if (!button) return;
  if (busy) {
    if (button.dataset.idleLabel === undefined) {
      button.dataset.idleLabel = button.textContent;
      const width = Math.ceil(button.getBoundingClientRect().width);
      if (width > 0) button.style.minWidth = width + 'px';
    }
    button.disabled = true;
    button.textContent = label || (button.dataset.idleLabel + '…');
  } else {
    if (button.dataset.idleLabel !== undefined) button.textContent = button.dataset.idleLabel;
    delete button.dataset.idleLabel;
    button.style.minWidth = '';
    button.disabled = false;
  }
}
function setFieldValue(id, value) {
  const el = $(id);
  if (el && document.activeElement !== el && el.value !== value) el.value = value;
}
function skeletonBox(el, rows) {
  if (!el || el.childElementCount) return;
  let html = '<div class="skeleton-stack" aria-hidden="true">';
  for (let i = 0; i < (rows || 3); i++) {
    html += '<div class="skeleton skeleton-line' + (i === 0 ? ' medium' : i === (rows || 3) - 1 ? ' short' : '') + '"></div>';
  }
  el.innerHTML = html + '</div>';
}
function skeletonRows(el, colspan, rows) {
  if (!el || el.childElementCount) return;
  let html = '';
  for (let i = 0; i < (rows || 4); i++) {
    html += '<tr><td colspan="' + (colspan || 1) + '"><div class="skeleton skeleton-line' + (i === (rows || 4) - 1 ? ' short' : '') + '"></div></td></tr>';
  }
  el.innerHTML = html;
}
function agoText(ts) {
  const seconds = Math.max(0, Math.round((Date.now() - ts) / 1000));
  if (seconds < 60) return seconds + ' 秒';
  return Math.round(seconds / 60) + ' 分钟';
}
const memoryCache = new Map();
function cacheKeyFor(key) { return CACHE_PREFIX + key; }
function readCache(key) {
  const fullKey = cacheKeyFor(key);
  let entry = memoryCache.get(fullKey);
  if (!entry) {
    try {
      const raw = sessionStorage.getItem(fullKey);
      if (raw) entry = JSON.parse(raw);
    } catch (e) { entry = null; }
  }
  if (!entry || typeof entry.ts !== 'number' || entry.v === undefined) return null;
  memoryCache.set(fullKey, entry);
  return entry;
}
function writeCache(key, payload) {
  const fullKey = cacheKeyFor(key);
  const entry = { ts: Date.now(), v: payload };
  memoryCache.set(fullKey, entry);
  try {
    sessionStorage.setItem(fullKey, JSON.stringify(entry));
  } catch (e) {
    try {
      const keys = [];
      for (let i = 0; i < sessionStorage.length; i++) {
        const name = sessionStorage.key(i);
        if (name && name.indexOf(CACHE_PREFIX) === 0) keys.push(name);
      }
      if (keys.length) sessionStorage.removeItem(keys[0]);
      sessionStorage.setItem(fullKey, JSON.stringify(entry));
    } catch (e2) {}
  }
}
function invalidateCache(key) {
  const fullKey = cacheKeyFor(key);
  memoryCache.delete(fullKey);
  try { sessionStorage.removeItem(fullKey); } catch (e) {}
}
function invalidateAll() {
  memoryCache.clear();
  try {
    const keys = [];
    for (let i = 0; i < sessionStorage.length; i++) {
      const name = sessionStorage.key(i);
      if (name && name.indexOf(CACHE_PREFIX) === 0) keys.push(name);
    }
    keys.forEach(name => sessionStorage.removeItem(name));
  } catch (e) {}
}
async function loadCached(key, ttl, options) {
  const opts = options || {};
  const force = !!opts.force;
  const revalidate = force || !!opts.revalidate;
  const entry = readCache(key);
  const fresh = entry && !revalidate && Date.now() - entry.ts < ttl;
  const controller = new AbortController();
  if (opts.signal) {
    if (opts.signal.aborted) throw apiError('请求已取消', 'abort');
    opts.signal.addEventListener('abort', () => controller.abort(), { once: true });
  }
  if (opts.track) {
    const previous = viewControllers.get(opts.track);
    if (previous) previous.abort();
    viewControllers.set(opts.track, controller);
  }
  const deliver = (payload, meta) => { if (opts.render) opts.render(payload, meta); };
  const meta = { cached: true, entry: entry, ttl: ttl, stale: !!entry && Date.now() - entry.ts >= ttl };
  if (fresh) { deliver(entry.v, meta); return entry.v; }
  if (entry) deliver(entry.v, meta);
  try {
    const payload = await api(opts.path, { signal: controller.signal });
    writeCache(key, payload);
    if (!controller.signal.aborted) deliver(payload, { cached: false, force: force });
    return payload;
  } catch (error) {
    if (!entry || isAbort(error)) throw error;
    if (!controller.signal.aborted) deliver(entry.v, Object.assign({}, meta, { error: error }));
    return entry.v;
  }
}
function forceParam(path) { return path + (path.indexOf('?') >= 0 ? '&' : '?') + 'refresh=1'; }
function staleText(meta) {
  if (!meta || !meta.stale || !meta.entry) return '';
  return '数据更新于 ' + agoText(meta.entry.ts) + ' 前 · 正在后台更新…';
}
function staleNote(meta) {
  const text = staleText(meta);
  return text ? '<div class="muted">' + esc(text) + '</div>' : '';
}
function retryButton(el, handler) {
  const button = el && el.querySelector('[data-retry]');
  if (button) button.addEventListener('click', handler, { once: true });
}
const TAB_TRACKS = {
  overview: ['account', 'models', 'announcements', 'checkinStatus'],
  dashboard: ['dashboard'],
  models: ['models', 'apiKeys'],
  logs: ['logs'],
  account: ['account', 'checkinStatus'],
  settings: ['settings']
};
function cancelInactiveTabs(tab) {
  const keep = TAB_TRACKS[tab] || [];
  viewControllers.forEach((controller, track) => {
    if (keep.indexOf(track) === -1) {
      controller.abort();
      viewControllers.delete(track);
    }
  });
}
function activateTab(tab) {
  if (!tab) return;
  state.activeTab = tab;
  cancelInactiveTabs(tab);
  document.querySelectorAll('.tabs button').forEach(item => item.classList.toggle('active', item.dataset.tab === tab));
  document.querySelectorAll('.page').forEach(page => page.classList.toggle('active', page.id === 'page-' + tab));
  if (tab === 'dashboard') loadDashboard();
  if (tab === 'logs') loadLogs(state.logsPage || 0);
  if (tab === 'models') { loadModels(); loadAPIKeys(); }
  if (tab === 'account') { queryAccount(); loadCheckinStatus(); }
  if (tab === 'settings') loadSettings();
  if (tab === 'overview') { queryAccount(); loadAnnouncements(); }
}
document.querySelectorAll('.tabs button').forEach(button => {
  button.addEventListener('click', () => activateTab(button.dataset.tab));
});
async function withBusy(button, label, task) {
  setBusy(button, true, label);
  try { return await task(); } finally { setBusy(button, false); }
}
function wireRetry(root, retry) {
  if (!root || !retry) return;
  const button = root.querySelector('[data-retry]');
  if (button) button.onclick = retry;
}
function clearLoadErrors(el) {
  if (!el) return;
  el.querySelectorAll('[data-load-error]').forEach(node => node.remove());
}
function gridBanner(el, error, retry) {
  if (!el) return;
  clearLoadErrors(el);
  const wrap = document.createElement('div');
  wrap.innerHTML = '<div class="banner error" data-load-error="1" style="grid-column:1/-1">' + esc(describeError(error))
    + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>';
  const node = wrap.firstChild;
  el.insertBefore(node, el.firstChild);
  wireRetry(node, retry);
}
function inlineError(el, error, retry, colspan) {
  if (!el) return;
  const loaded = el.dataset.loaded === '1';
  clearLoadErrors(el);
  const message = describeError(error);
  if (colspan) {
    const row = document.createElement('tr');
    row.innerHTML = '<td colspan="' + colspan + '"><div class="banner error" data-load-error="1">' + esc(message)
      + ' <button type="button" class="small ghost" data-retry="1">重试</button></div></td>';
    if (!loaded) el.innerHTML = '';
    el.insertBefore(row, el.firstChild);
    wireRetry(row, retry);
    return;
  }
  const wrap = document.createElement('div');
  wrap.innerHTML = '<div class="banner error" data-load-error="1">' + esc(message)
    + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>';
  const node = wrap.firstChild;
  if (!loaded) el.innerHTML = '';
  el.insertBefore(node, el.firstChild);
  wireRetry(node, retry);
}

let settingsDirty = false;
function renderSettings(payload, meta) {
  const error = meta && meta.error ? meta.error : null;
  state.settings = payload;
  $('baseURL').textContent = payload.base_url || '';
  if (!settingsDirty) {
    setFieldValue('cfgBaseURL', payload.base_url || '');
    setFieldValue('cfgUserAgent', payload.user_agent || '');
    setFieldValue('cfgProxyURL', payload.proxy_url || '');
    setFieldValue('cfgUsername', payload.username || '');
  }
  const status = 'API Key：' + (payload.has_api_key ? '<span class="ok">已配置 ' + esc(payload.api_key_masked || '') + '</span>' : '<span class="err">未配置</span>')
    + ' · 账号：' + (payload.has_account ? '<span class="ok">已授权</span>' : '<span class="err">未授权</span>')
    + ' · 数据目录：<code>' + esc(payload.data_dir || '') + '</code>';
  $('settingsMeta').innerHTML = status + staleNote(meta);
  $('settingsMeta').dataset.loaded = '1';
  if (error) inlineError($('settingsMeta'), error, () => loadSettings({ force: true }));
  const dot = $('healthDot'), text = $('healthText');
  dot.className = 'status-dot' + (payload.has_api_key && payload.has_account ? ' on' : '');
  text.textContent = payload.has_api_key ? (payload.has_account ? '推理与账号均已就绪' : '推理就绪，账号未授权') : 'API Key 未配置';
}
async function loadSettings(options) {
  const opts = options || {};
  const force = !!opts.force;
  try {
    return await loadCached('settings', CACHE_TTL, {
      path: force ? forceParam(API.settings) : API.settings,
      force: force,
      track: 'settings',
      render: renderSettings
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const meta = $('settingsMeta');
    if (meta) {
      meta.innerHTML = '';
      gridBanner(meta, error, () => loadSettings({ force: true }));
    }
    $('healthDot').className = 'status-dot';
    $('healthText').textContent = isUnauthorized(error) ? '未授权' : (isNetworkError(error) ? '网关不可达' : '加载失败');
    return null;
  }
}

function renderModels(payload, meta) {
  clearLoadErrors($('modelsRows'));
  clearLoadErrors($('overviewModels'));
  state.models = payload.models || [];
  $('modelsCount').textContent = '（' + state.models.length + '）';
  $('overviewModelCount').textContent = state.models.length ? state.models.length + ' 个可用模型' : '';
  $('modelsMeta').innerHTML = (payload.last_sync ? ('上次同步：' + esc(payload.last_sync) + (payload.last_error ? ' · ' + esc(payload.last_error) : '')) : '尚未从 AgentRouter 同步模型列表') + staleNote(meta);
  const modelsRows = $('modelsRows'), overviewModels = $('overviewModels');
  if (modelsRows) {
    modelsRows.innerHTML = state.models.map(model =>
      '<tr><td>' + esc(model.name) + '<div class="muted">' + esc(model.display_name !== model.name ? model.display_name : (model.client_name || '')) + '</div></td>'
      + '<td>' + (model.protocols || [model.protocol]).map(protocol => '<span class="pill">' + esc(protocol) + '</span>').join('') + '</td>'
      + '<td>' + (model.context_length ? fmtNum(model.context_length) : '-') + '</td>'
      + '<td>' + (model.input_modalities || []).map(item => '<span class="pill' + (item === 'image' ? ' ok' : '') + '">' + esc(item) + '</span>').join('') + '</td></tr>'
    ).join('') || '<tr><td colspan="4" class="empty">暂无模型</td></tr>';
    modelsRows.dataset.loaded = '1';
  }
  if (overviewModels) {
    overviewModels.innerHTML = state.models.length
      ? '<table><tbody>' + state.models.slice(0, 8).map(model =>
          '<tr><td>' + esc(model.name) + '</td><td>' + (model.protocols || [model.protocol]).map(protocol => '<span class="pill">' + esc(protocol) + '</span>').join('') + '</td><td class="muted">' + (model.context_length ? fmtNum(model.context_length) : '-') + '</td></tr>'
        ).join('') + '</tbody></table>'
      : '<div class="empty">尚未同步模型</div>';
    overviewModels.dataset.loaded = '1';
  }
  if (meta && meta.error) {
    inlineError($('modelsRows'), meta.error, () => loadModels({ force: true }), 4);
    inlineError($('overviewModels'), meta.error, () => loadModels({ force: true }));
  }
}
async function loadModels(options) {
  const opts = options || {};
  const force = !!opts.force;
  skeletonRows($('modelsRows'), 4, 4);
  try {
    return await loadCached('models', CACHE_TTL, {
      path: force ? forceParam(API.models) : API.models,
      force: force,
      track: 'models',
      render: renderModels
    });
  } catch (error) {
    if (isAbort(error)) return null;
    inlineError($('modelsRows'), error, () => loadModels({ force: true }), 4);
    inlineError($('overviewModels'), error, () => loadModels({ force: true }));
    $('modelsMeta').textContent = describeError(error);
    return null;
  }
}

function checkinMetric(info) {
  if (info && info.checked_in) return ['<span class="ok">已签到</span>', info.checked_in_at || ''];
  if (info && info.checked_in_known === false) return ['<span class="warn">未知</span>', '签到记录暂时读取不到'];
  return ['<span class="warn">未签到</span>', ''];
}
function renderAccount(info, meta) {
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
  state.account = info;
  if (info.success) {
    const checkin = checkinMetric(info);
    metrics.innerHTML = [
      ['当前余额', fmtUSD(info.quota_usd), info.group ? ('分组 ' + info.group) : ''],
      ['历史消耗', fmtUSD(info.used_usd), ''],
      ['请求次数', String(info.request_count || 0), ''],
      ['今日签到', checkin[0], checkin[1]]
    ].map(([label, value, sub]) => '<div class="metric"><div class="label">' + label + '</div><div class="value">' + value + '</div><div class="sub">' + esc(sub) + '</div></div>').join('');
  } else {
    metrics.innerHTML = '';
  }
  const rows = [
    ['账号', esc(info.display_name || info.username || '-')],
    ['用户 ID', esc(info.user_id || '-')],
    ['分组', esc(info.group || '-')],
    ['可用模型', esc((info.models || []).join(', ') || '-')],
    ['推理密钥', esc(info.api_key_masked || '-')],
    ['授权状态', info.authorized ? '<span class="ok">已授权</span>' : '<span class="err">未授权</span>']
  ];
  if (info.stale) rows.push(['数据时间', esc(info.stale_at || '缓存') + '（控制台不可达，已显示缓存）']);
  detail.innerHTML = staleNote(meta) + rows.map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('');
  if (info.message) detail.innerHTML += '<div class="banner warn">' + esc(info.message) + '</div>';
  if (meta && meta.error) {
    detail.innerHTML += '<div class="banner error">' + esc(describeError(meta.error))
      + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>';
    retryButton(detail, () => queryAccount({ force: true }));
  }
  detail.dataset.loaded = '1';
  const accountState = $('accountStateBox');
  if (accountState) {
    accountState.innerHTML = [
      ['账号', esc(info.display_name || info.username || '-')],
      ['用户 ID', esc(info.user_id || '-')],
      ['分组', esc(info.group || '-')],
      ['余额', fmtUSD(info.quota_usd)],
      ['授权', info.authorized ? '<span class="ok">是</span>' : '<span class="err">否</span>']
    ].map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('')
      + (info.message ? '<div class="banner warn">' + esc(info.message) + '</div>' : '');
    accountState.dataset.loaded = '1';
  }
  $('overviewBanner').innerHTML = info.authorized ? '' : '<div class="banner warn">账号尚未授权。前往「账号与授权」登录，即可查看余额、日志、模型状态与签到。</div>';
}

async function queryAccount(options) {
  const opts = options || {};
  const force = !!opts.force;
  try {
    return await loadCached('account', CACHE_TTL, {
      path: force ? forceParam(API.account) : API.account,
      force: force,
      track: 'account',
      render: (payload, meta) => renderAccount(payload.account || { message: '暂无账号数据' }, meta)
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const detail = $('overviewAccount');
    const metrics = $('overviewMetrics');
    if (!metrics) return null;
    const loaded = detail && detail.dataset.loaded === '1';
    if (!loaded) {
      metrics.innerHTML = '';
      if (detail) detail.innerHTML = '';
    }
    inlineError($('overviewAccount'), error, () => queryAccount({ force: true }));
    const accountState = $('accountStateBox');
    if (accountState && accountState.dataset.loaded !== '1') inlineError(accountState, error, () => queryAccount({ force: true }));
    return null;
  }
}

function checkinRows(payload, optimistic) {
  const rows = [];
  const known = payload.checked_in_known !== false;
  const checked = optimistic ? true : !!payload.checked_in;
  const when = payload.checked_in_at ? esc(payload.checked_in_at) : '';
  let today;
  if (optimistic && !payload.checked_in) today = '<span class="warn">签到中…</span>';
  else if (checked) today = '<span class="ok">已签到</span>' + (when ? ' <span class="muted">' + when + '</span>' : '');
  else if (!known) today = '<span class="warn">未知</span> <span class="muted">签到记录暂时读取不到</span>';
  else today = '<span class="warn">未签到</span>';
  if (checked && payload.reward_usd) today += ' <span class="muted">奖励 ' + fmtUSD(payload.reward_usd) + '</span>';
  rows.push(['今日签到', today]);
  if (payload.last_checkin) {
    rows.push(['最近签到', esc(payload.last_checkin.checked_in_at || '') + (payload.last_checkin.quota_awarded ? ' · 奖励 ' + fmtUSD(payload.last_checkin.quota_awarded) : '')]);
  }
  if (payload.server_error && !checked) rows.push(['服务端', '<span class="warn">' + esc(payload.server_error) + '</span>']);
  return rows;
}
function renderCheckinStatus(payload, meta, optimistic) {
  const rows = checkinRows(payload, optimistic);
  const html = staleNote(meta) + rows.map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('');
  const statusRows = rows.filter(([key]) => key !== '今日签到');
  const statusHtml = staleNote(meta) + statusRows.map(([key, value]) => '<div class="stat"><span class="muted">' + key + '</span><span>' + value + '</span></div>').join('');
  const status = $('checkinStatusBox');
  if (status) {
    status.innerHTML = statusHtml;
    status.dataset.loaded = '1';
    if (meta && meta.error) {
      status.insertAdjacentHTML('beforeend', '<div class="banner error">' + esc(describeError(meta.error)) + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>');
      retryButton(status, () => loadCheckinStatus({ force: true }));
    }
  }
  const box = $('checkinBox');
  if (box && box.dataset.keep !== '1') {
    box.innerHTML = html + (payload.last_checkin ? '<div class="stat"><span class="muted">消息</span><span>' + esc(payload.last_checkin.message) + '</span></div>' : '');
    box.dataset.loaded = '1';
  }
}
async function loadCheckinStatus(options) {
  const opts = options || {};
  const force = !!opts.force;
  try {
    return await loadCached('checkinStatus', 60000, {
      path: force ? forceParam(API.checkinStatus) : API.checkinStatus,
      force: force,
      track: 'checkinStatus',
      render: (payload, meta) => renderCheckinStatus(payload, meta)
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const status = $('checkinStatusBox');
    if (status) {
      inlineError(status, error, () => loadCheckinStatus({ force: true }));
      status.dataset.loaded = '1';
    }
    if ($('checkinBox') && $('checkinBox').dataset.keep !== '1') inlineError($('checkinBox'), error, () => loadCheckinStatus({ force: true }));
    return null;
  }
}

let checkinRunning = false;
function checkinBanner(kind, title, detail) {
  return '<div class="banner ' + kind + '"><strong>' + esc(title) + '</strong>'
    + (detail ? '<div class="muted">' + esc(detail) + '</div>' : '') + '</div>';
}
function setCheckinBox(html) {
  const box = $('checkinBox');
  if (!box) return;
  box.dataset.keep = '1';
  box.innerHTML = html;
}
async function runCheckin(button) {
  if (checkinRunning) return;
  checkinRunning = true;
  setBusy(button, true, '签到中…');
  const other = button && button.id === 'checkinBtn' ? $('accountCheckinBtn') : $('checkinBtn');
  if (other) setBusy(other, true, '签到中…');
  const cached = readCache('checkinStatus');
  if (cached) renderCheckinStatus(cached.v, { cached: true, entry: cached }, true);
  setCheckinBox(checkinBanner('', '正在签到…', '正在退出并重新登录 AgentRouter，请稍候。'));
  try {
    const payload = await api(API.checkin, { method: 'POST', body: JSON.stringify({}) });
    const result = payload.result || {};
    const titled = result.message || '签到成功';
    setCheckinBox(checkinBanner('', titled, result.quota_awarded ? '奖励 ' + fmtUSD(result.quota_awarded / 500000) : ''));
    toast(titled);
    invalidateCache('checkinStatus');
    invalidateCache('account');
    invalidateCache('settings');
    state.account = null;
    await Promise.all([queryAccount({ force: true }), loadCheckinStatus({ force: true })]);
    const box = $('checkinBox');
    if (box) box.dataset.keep = '0';
  } catch (error) {
    const message = describeError(error);
    setCheckinBox(checkinBanner('error', '签到失败', message));
    toast(message, true);
    if (cached) renderCheckinStatus(cached.v, { cached: true, entry: cached });
  } finally {
    checkinRunning = false;
    setBusy(button, false);
    if (other) setBusy(other, false);
  }
}

async function refreshModels(button) {
  await withBusy(button, '同步中…', async () => {
    try {
      const payload = await api(API.refreshModels, { method: 'POST', body: JSON.stringify({}) });
      toast('已同步 ' + payload.count + ' 个模型');
      invalidateCache('models');
      invalidateCache('account');
      await loadModels({ force: true });
    } catch (error) { toast('同步失败：' + describeError(error), true); }
  });
}

async function runTest() {
  const button = $('testBtn');
  const output = $('testResult');
  await withBusy(button, '测试中…', async () => {
    output.innerHTML = '<div class="muted">测试中…</div>';
    try {
      const payload = await api(API.test, { method: 'POST', body: JSON.stringify({ model: $('testModel').value.trim() }) });
      output.innerHTML = '<div class="ok">' + esc(payload.model) + ' · ' + esc(payload.protocol) + ' · ' + esc(payload.latency_ms) + ' ms</div><pre>' + esc(payload.response || '') + '</pre>';
    } catch (error) {
      output.innerHTML = '<div class="err">' + esc(describeError(error)) + '</div>';
    }
  });
}

function renderBars(container, values, format) {
  if (!container) return;
  const max = Math.max(1, ...values.map(item => item.value));
  container.innerHTML = values.map(item =>
    '<div class="bar" style="height:' + Math.max(2, Math.round(item.value / max * 100)) + '%"><span>' + esc(item.label) + '：' + format(item.value) + '</span></div>'
  ).join('') || '<div class="empty">暂无数据</div>';
}

function renderDashboard(payload, meta) {
  const metrics = $('dashboardMetrics');
  if (!metrics) return;
  clearLoadErrors(metrics);
  const stale = staleNote(meta);
  if (!payload.success) {
    metrics.innerHTML = (meta && meta.error
      ? '<div class="banner error" style="grid-column:1/-1">' + esc(describeError(meta.error)) + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>'
      : '<div class="banner warn" style="grid-column:1/-1">' + esc(payload.message || '账号尚未授权') + '</div>');
    retryButton(metrics, () => loadDashboard({ force: true, days: state.dashboardDays }));
    $('chartCost').innerHTML = '';
    $('chartCount').innerHTML = '';
    $('dashboardByModel').innerHTML = '<tr><td colspan="4" class="empty">暂无数据</td></tr>';
    return;
  }

  state.dashboard = payload;
  metrics.innerHTML = [
    ['当前余额', fmtUSD(payload.quota_usd), payload.group ? '分组 ' + payload.group : ''],
    ['区间消费', fmtUSD(payload.total_usd), '最近 ' + payload.days + ' 天'],
    ['请求次数', fmtNum(payload.total_count), ''],
    ['Tokens', fmtNum(payload.total_tokens), '']
  ].map(([label, value, sub]) => '<div class="metric"><div class="label">' + label + '</div><div class="value">' + value + '</div><div class="sub">' + esc(sub) + '</div></div>').join('');
  if (stale) metrics.innerHTML = '<div class="muted" style="grid-column:1/-1">' + esc(staleText(meta)) + '</div>' + metrics.innerHTML;
  const timeline = payload.timeline || [];
  renderBars($('chartCost'), timeline.map(item => ({ label: item.time, value: item.quota_usd })), fmtUSD);
  renderBars($('chartCount'), timeline.map(item => ({ label: item.time, value: item.count })), value => fmtNum(value));
  $('dashboardByModel').innerHTML = (payload.by_model || []).slice().sort((a, b) => b.quota_usd - a.quota_usd).map(item =>
    '<tr><td>' + esc(item.model) + '</td><td>' + fmtNum(item.count) + '</td><td>' + fmtNum(item.tokens) + '</td><td>' + fmtUSD(item.quota_usd) + '</td></tr>'
  ).join('') || '<tr><td colspan="4" class="empty">暂无数据</td></tr>';
  if (meta && meta.error) inlineError(metrics, meta.error, () => loadDashboard({ force: true, days: state.dashboardDays }));
}
async function loadDashboard(options) {
  const opts = options || {};
  const days = opts.days || ($('dashboardDays') ? $('dashboardDays').value : '7');
  state.dashboardDays = days;
  const force = !!opts.force;
  if (!force) skeletonBox($('dashboardMetrics'), 4);
  const path = API.dashboard + '?days=' + encodeURIComponent(days) + (force ? '&refresh=1' : '');
  try {
    return await loadCached('dashboard:' + days, CACHE_TTL, {
      path: path,
      force: force,
      track: 'dashboard',
      render: renderDashboard
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const metrics = $('dashboardMetrics');
    if (metrics) {
      inlineError(metrics, error, () => loadDashboard({ force: true, days: days }));
      metrics.dataset.loaded = '1';
    }
    return null;
  }
}

let logsRequestSeq = 0;
function logTypeClass(type) {
  if (type === 5) return 'err';
  if (type === 4) return 'ok';
  if (type === 2) return 'spend';
  if (type === 1) return 'recharge';
  return '';
}
function logDetailText(item) {
  const text = String(item.content || '').trim();
  return text || '-';
}
function renderLogs(payload, meta) {
  const box = $('logsBox');
  if (!box) return;
  if (!payload.success) {
    box.innerHTML = '<div class="banner warn">' + esc(payload.message || '账号尚未授权') + '</div>';
    $('logsMeta').textContent = '';
    return;
  }
  state.logsPage = payload.page ? payload.page - 1 : 0;
  state.logsTotal = payload.total || 0;
  $('logsMeta').textContent = '共 ' + state.logsTotal + ' 条 · 第 ' + (state.logsPage + 1) + ' 页' + (meta && meta.stale ? ' · 更新于 ' + agoText(meta.entry.ts) + ' 前' : '');
  const rows = (payload.items || []).map(item => {
    const tokens = (item.prompt_tokens || 0) + (item.completion_tokens || 0);
    const detail = logDetailText(item);
    const model = item.model_name || '-';
    const cost = Number(item.quota_usd || 0);
    return '<tr>'
      + '<td class="log-time">' + esc(item.created_at_text) + '</td>'
      + '<td class="log-model"><span title="' + esc(model) + '">' + esc(model) + '</span></td>'
      + '<td class="log-type"><span class="pill ' + logTypeClass(item.type) + '">' + esc(item.type_label) + '</span></td>'
      + '<td class="log-num">' + esc(fmtNum(tokens)) + '</td>'
      + '<td class="log-cost' + (cost > 0 ? '' : ' zero') + '">' + esc(fmtUSD(cost)) + '</td>'
      + '<td class="log-detail"><span title="' + esc(detail) + '">' + esc(detail) + '</span></td>'
      + '</tr>';
  }).join('');
  box.innerHTML = rows ? '<table class="logs-table"><thead><tr><th>时间</th><th>模型</th><th>类型</th><th class="right">Tokens</th><th class="right">消费</th><th>详情</th></tr></thead><tbody>' + rows + '</tbody></table>' : '<div class="empty">没有匹配的日志</div>';
  box.dataset.loaded = '1';
  if (meta && meta.error) inlineError(box, meta.error, () => loadLogs(state.logsPage, true));
}
async function loadLogs(page, force) {
  const days = Number($('logRange').value || 7);
  const type = $('logType').value;
  const bucket = Math.floor(Date.now() / 60000);
  const seq = ++logsRequestSeq;
  if (!force) skeletonRows($('logsBox'), 6, 5);
  const query = '?page=' + page + '&page_size=20&type=' + encodeURIComponent(type)
    + '&start=' + (bucket * 60 - days * 86400) + '&end=' + (bucket * 60) + (force ? '&refresh=1' : '');
  try {
    return await loadCached('logs:' + type + ':' + days + ':' + page + ':' + (force ? seq : bucket), CACHE_TTL, {
      path: API.logs + query,
      force: !!force,
      track: 'logs',
      render: (payload, meta) => { if (seq === logsRequestSeq) renderLogs(payload, meta); }
    });
  } catch (error) {
    if (isAbort(error) || seq !== logsRequestSeq) return null;
    inlineError($('logsBox'), error, () => loadLogs(page, true), 6);
    return null;
  }
}

function renderAPIKeys(payload, meta) {
  const box = $('apiKeysBox');
  if (!box) return;
  clearLoadErrors(box);
  if (!payload.success) {
    box.innerHTML = '<div class="banner warn">' + esc(payload.message || '账号尚未授权') + '</div>';
    return;
  }
  box.innerHTML = (meta && meta.stale ? '<div class="muted">更新于 ' + agoText(meta.entry.ts) + ' 前</div>' : '')
    + '<table><thead><tr><th>名称</th><th>密钥</th><th>状态</th><th>用量</th></tr></thead><tbody>'
    + (payload.items || []).map(token =>
      '<tr><td>' + esc(token.name) + (token.in_use ? ' <span class="pill ok">使用中</span>' : '') + '</td>'
      + '<td><code>' + esc(token.key_masked) + '</code></td>'
      + '<td>' + (token.status === 1 ? '<span class="pill ok">启用</span>' : '<span class="pill err">停用</span>') + '</td>'
      + '<td>' + fmtUSD(token.used_usd) + '</td></tr>'
    ).join('') + '</tbody></table>';
  box.dataset.loaded = '1';
  if (meta && meta.error) inlineError(box, meta.error, () => loadAPIKeys({ force: true }));
}
async function loadAPIKeys(options) {
  const opts = options || {};
  const force = !!opts.force;
  if (!force && $('apiKeysBox') && !$('apiKeysBox').childElementCount) skeletonBox($('apiKeysBox'), 4);
  try {
    return await loadCached('apiKeys', CACHE_TTL, {
      path: force ? forceParam(API.apiKeys) : API.apiKeys,
      force: force,
      track: 'apiKeys',
      render: renderAPIKeys
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const box = $('apiKeysBox');
    if (box) { inlineError(box, error, () => loadAPIKeys({ force: true })); box.dataset.loaded = '1'; }
    return null;
  }
}

function renderAnnouncements(payload, meta) {
  const box = $('overviewAnnouncements');
  if (!box) return;
  if (!payload.success) {
    box.innerHTML = '<div class="empty">' + esc(payload.message || '公告不可用') + '</div>'
      + (meta && meta.error ? '<div class="banner error">' + esc(describeError(meta.error)) + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>' : '');
    retryButton(box, () => loadAnnouncements({ force: true }));
    return;
  }
  box.innerHTML = (payload.announcements || []).slice(0, 6).map(item =>
    '<div class="stat" style="display:block"><div class="muted">' + esc(item.publish_date) + '</div><div style="white-space:pre-wrap;font-size:13px;margin-top:4px">' + esc(item.content.length > 400 ? item.content.slice(0, 400) + '…' : item.content) + '</div></div>'
  ).join('') || '<div class="empty">暂无公告</div>';
  box.dataset.loaded = '1';
  if (meta && meta.error) {
    box.insertAdjacentHTML('afterbegin', '<div class="banner error">' + esc(describeError(meta.error)) + ' <button type="button" class="small ghost" data-retry="1">重试</button></div>');
    retryButton(box, () => loadAnnouncements({ force: true }));
  }
}
async function loadAnnouncements(options) {
  const opts = options || {};
  const force = !!opts.force;
  try {
    return await loadCached('announcements', 120000, {
      path: force ? forceParam(API.announcements) : API.announcements,
      force: force,
      track: 'announcements',
      render: renderAnnouncements
    });
  } catch (error) {
    if (isAbort(error)) return null;
    const box = $('overviewAnnouncements');
    if (box) {
      inlineError(box, error, () => loadAnnouncements({ force: true }));
      box.dataset.loaded = '1';
    }
    return null;
  }
}

function loginBanner(kind, title, detail) {
  return '<div class="banner ' + kind + '"><strong>' + esc(title) + '</strong>'
    + (detail ? '<div class="muted">' + esc(detail) + '</div>' : '')
    + (kind === 'error' && detail ? ' <button type="button" class="small ghost" data-retry="1">重试</button>' : '')
    + '</div>';
}
function refreshAfterAuthChanged() {
  invalidateAll();
  state.models = [];
  state.account = null;
  loadSettings({ force: true });
  queryAccount({ force: true });
  loadModels({ force: true });
  loadCheckinStatus({ force: true });
  loadAnnouncements({ force: true });
}
async function doLogin() {
  const button = $('loginBtn');
  const result = $('loginResult');
  await withBusy(button, '登录中…', async () => {
    result.textContent = '正在登录 AgentRouter…';
    try {
      const payload = await api(API.login, { method: 'POST', body: JSON.stringify({ username: $('loginUsername').value.trim(), password: $('loginPassword').value }) });
      result.innerHTML = loginBanner('', payload.message || '登录成功', payload.api_key_name ? '推理密钥：' + payload.api_key_name : '');
      $('loginPassword').value = '';
      toast('授权成功');
      refreshAfterAuthChanged();
    } catch (error) {
      result.innerHTML = loginBanner('error', '登录失败', describeError(error));
      wireRetry(result, doLogin);
    }
  });
}

async function saveSession() {
  const button = $('sessionBtn');
  const result = $('loginResult');
  await withBusy(button, '保存中…', async () => {
    try {
      const payload = await api(API.token, { method: 'POST', body: JSON.stringify({ session: $('loginSession').value.trim(), user_id: $('loginUserID').value.trim() }) });
      result.innerHTML = loginBanner('', payload.message || '会话已保存', '');
      $('loginSession').value = '';
      toast('浏览器会话已保存');
      refreshAfterAuthChanged();
    } catch (error) {
      result.innerHTML = loginBanner('error', '保存会话失败', describeError(error));
      wireRetry(result, saveSession);
    }
  });
}

async function saveToken() {
  const button = $('tokenBtn');
  const result = $('loginResult');
  await withBusy(button, '保存中…', async () => {
    try {
      const payload = await api(API.token, { method: 'POST', body: JSON.stringify({ token: $('loginToken').value.trim(), user_id: $('loginUserID').value.trim() }) });
      result.innerHTML = loginBanner('', payload.message || '令牌已保存', '');
      $('loginToken').value = '';
      toast('访问令牌已保存');
      refreshAfterAuthChanged();
    } catch (error) {
      result.innerHTML = loginBanner('error', '保存令牌失败', describeError(error));
      wireRetry(result, saveToken);
    }
  });
}

async function saveSettings() {
  const button = $('settingsSave');
  const body = {
    base_url: $('cfgBaseURL').value.trim(),
    user_agent: $('cfgUserAgent').value.trim(),
    proxy_url: $('cfgProxyURL').value.trim(),
    username: $('cfgUsername').value.trim()
  };
  if ($('cfgAPIKey').value.trim()) body.api_key = $('cfgAPIKey').value.trim();
  if ($('cfgPassword').value) body.password = $('cfgPassword').value;
  await withBusy(button, '保存中…', async () => {
    try {
      const payload = await api(API.settings, { method: 'POST', body: JSON.stringify(body) });
      $('cfgAPIKey').value = '';
      $('cfgPassword').value = '';
      settingsDirty = false;
      toast(payload.message || '配置已保存');
      invalidateCache('settings');
      invalidateCache('models');
      invalidateCache('account');
      await loadSettings({ force: false });
      await loadModels({ force: true });
    } catch (error) {
      gridBanner($('settingsMeta'), error, saveSettings);
      toast('保存失败：' + describeError(error), true);
    }
  });
}

async function loadAll(force) {
  const opts = { force: !!force };
  queryAccount(opts);
  loadModels(opts);
  loadAnnouncements(opts);
  loadCheckinStatus(opts);
  loadSettings(opts);
  if (state.activeTab === 'dashboard') loadDashboard({ force: !!force });
  if (state.activeTab === 'logs') loadLogs(state.logsPage || 0, !!force);
  if (state.activeTab === 'models') loadAPIKeys(opts);
}

let dashboardDebounce = null;
function scheduleDashboard(days) {
  clearTimeout(dashboardDebounce);
  dashboardDebounce = setTimeout(() => { dashboardDebounce = null; loadDashboard({ days: days }); }, 280);
}
function debouncedDashboard() {
  const days = $('dashboardDays') ? $('dashboardDays').value : '7';
  scheduleDashboard(days);
}

$('refreshAll').addEventListener('click', () => withBusy($('refreshAll'), '刷新中…', () => loadAll(true)));
$('accountBtn').addEventListener('click', () => withBusy($('accountBtn'), '查询中…', () => queryAccount({ force: true })));
$('checkinStatusBtn').addEventListener('click', () => withBusy($('checkinStatusBtn'), '查询中…', () => loadCheckinStatus({ force: true })));
$('checkinBtn').addEventListener('click', () => runCheckin($('checkinBtn')));
$('accountCheckinBtn').addEventListener('click', () => runCheckin($('accountCheckinBtn')));
$('overviewRefreshModels').addEventListener('click', () => refreshModels($('overviewRefreshModels')));
$('modelsRefresh').addEventListener('click', () => refreshModels($('modelsRefresh')));
$('testBtn').addEventListener('click', runTest);
$('dashboardRefresh').addEventListener('click', () => withBusy($('dashboardRefresh'), '刷新中…', () => loadDashboard({ force: true })));
$('dashboardDays').addEventListener('change', debouncedDashboard);
$('logsRefresh').addEventListener('click', () => withBusy($('logsRefresh'), '查询中…', () => loadLogs(0, true)));
$('logType').addEventListener('change', () => loadLogs(0));
$('logRange').addEventListener('change', () => loadLogs(0));
$('logsPrev').addEventListener('click', () => loadLogs(Math.max(0, state.logsPage - 1)));
$('logsNext').addEventListener('click', () => loadLogs(state.logsPage + 1));
$('loginBtn').addEventListener('click', doLogin);
$('tokenBtn').addEventListener('click', saveToken);
$('sessionBtn').addEventListener('click', saveSession);
$('settingsSave').addEventListener('click', saveSettings);
$('settingsReload').addEventListener('click', () => withBusy($('settingsReload'), '读取中…', () => loadSettings({ force: true })));

['cfgBaseURL', 'cfgUserAgent', 'cfgProxyURL', 'cfgUsername', 'cfgAPIKey', 'cfgPassword'].forEach(id => {
  const el = $(id);
  if (el) el.addEventListener('input', () => { settingsDirty = true; });
});
['loginUsername', 'loginPassword'].forEach(id => {
  const el = $(id);
  if (el) el.addEventListener('keydown', event => {
    if (event.key === 'Enter') { event.preventDefault(); doLogin(); }
  });
});
['loginToken', 'loginUserID', 'loginSession'].forEach(id => {
  const el = $(id);
  if (el) el.addEventListener('keydown', event => {
    if (event.key === 'Enter') { event.preventDefault(); saveToken(); }
  });
});

let pollTimer = null;
function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
}
function startPolling() {
  stopPolling();
  pollTimer = setInterval(() => {
    if (document.hidden) return;
    const active = document.querySelector('#page-dashboard.active');
    if (active) loadDashboard();
  }, POLL_INTERVAL);
}
document.addEventListener('visibilitychange', () => {
  if (document.hidden) {
    stopPolling();
    return;
  }
  startPolling();
  if (document.querySelector('#page-dashboard.active')) loadDashboard({ force: true });
});

loadAll(false);
startPolling();
`
