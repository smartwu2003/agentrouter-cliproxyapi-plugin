package main

// dashboardCSS holds the console stylesheet. Keep it dependency-free: the
// page is rendered inside a webview with no network access to CDNs. The
// 8px spacing scale, component variants, skeleton and chart styles are all
// defined here so the markup can stay mostly semantic.
const dashboardCSS = `
  :root {
    color-scheme: light dark;
    --bg: #f4f5f9;
    --bg-soft: #eceef4;
    --card: #ffffff;
    --card-2: #f8f9fc;
    --text: #171a21;
    --muted: #667085;
    --border: #e4e7ee;
    --border-strong: #cfd5e0;
    --accent: #4f46e5;
    --accent-btn: #4f46e5;
    --accent-btn-hover: #4338ca;
    --accent-soft: #eef2ff;
    --accent-text: #4338ca;
    --ok: #15803d;
    --ok-bg: rgba(22, 163, 74, .12);
    --warn: #b45309;
    --warn-bg: rgba(217, 119, 6, .13);
    --err: #b42318;
    --err-bg: rgba(220, 38, 38, .11);
    --chart: #6366f1;
    --chart-strong: #4f46e5;
    --chart-soft: rgba(99, 102, 241, .18);
    --grid-line: rgba(23, 26, 33, .07);
    --hover: rgba(79, 70, 229, .05);
    --ring: rgba(79, 70, 229, .22);
    --tooltip-bg: #171a21;
    --tooltip-text: #ffffff;
    --page-glow: rgba(99, 102, 241, .07);
    --shadow-sm: 0 1px 2px rgba(16, 24, 40, .05);
    --shadow-md: 0 10px 30px rgba(16, 24, 40, .08);
    --radius-lg: 16px;
    --radius: 12px;
    --radius-sm: 9px;
    --dur: 160ms;
    --ease: cubic-bezier(.4, 0, .2, 1);
  }
  @media (prefers-color-scheme: dark) {
    :root {
      color-scheme: dark;
      --bg: #0f1115;
      --bg-soft: #171a21;
      --card: #181b21;
      --card-2: #1e222a;
      --text: #eceff5;
      --muted: #98a2b3;
      --border: #2a2f38;
      --border-strong: #3a414d;
      --accent: #818cf8;
      --accent-btn: #5b5bd6;
      --accent-btn-hover: #6d6de0;
      --accent-soft: #242945;
      --accent-text: #c7d2fe;
      --ok: #4ade80;
      --ok-bg: rgba(74, 222, 128, .12);
      --warn: #fbbf24;
      --warn-bg: rgba(251, 191, 36, .12);
      --err: #f87171;
      --err-bg: rgba(248, 113, 113, .12);
      --chart: #818cf8;
      --chart-strong: #6366f1;
      --chart-soft: rgba(129, 140, 248, .22);
      --grid-line: rgba(255, 255, 255, .07);
      --hover: rgba(129, 140, 248, .08);
      --ring: rgba(129, 140, 248, .30);
      --tooltip-bg: #eceff5;
      --tooltip-text: #12141a;
      --page-glow: rgba(99, 102, 241, .10);
      --shadow-sm: 0 1px 2px rgba(0, 0, 0, .35);
      --shadow-md: 0 12px 32px rgba(0, 0, 0, .45);
    }
  }
  * { box-sizing: border-box; }
  html { -webkit-text-size-adjust: 100%; }
  body {
    margin: 0;
    min-height: 100vh;
    font: 14px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC", "Microsoft YaHei", sans-serif;
    color: var(--text);
    background-color: var(--bg);
    background-image: radial-gradient(1100px 560px at 50% -12%, var(--page-glow), transparent 62%);
    background-repeat: no-repeat;
    background-attachment: fixed;
    -webkit-font-smoothing: antialiased;
    text-rendering: optimizeLegibility;
  }
  .app { max-width: 1160px; margin: 0 auto; padding: 32px 24px 56px; }
  h1, h2, h3 { letter-spacing: -.01em; }
  h1 { font-size: 20px; line-height: 1.3; margin: 0; }
  h2 {
    display: flex; align-items: center; gap: 8px;
    font-size: 15px; font-weight: 650; line-height: 1.4;
    margin: 0 0 16px; padding-bottom: 12px;
    border-bottom: 1px solid var(--border);
  }
  h3 {
    display: flex; align-items: center; gap: 8px;
    font-size: 13px; font-weight: 650; color: var(--text);
    margin: 20px 0 10px;
  }
  h3::before {
    content: ""; flex: 0 0 auto; width: 3px; height: 13px;
    border-radius: 2px; background: var(--accent);
  }
  p { margin: 0; }
  .muted { color: var(--muted); font-size: 12px; }
  .mono { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace; font-size: 11.5px; }
  .ok { color: var(--ok); } .err { color: var(--err); } .warn { color: var(--warn); }
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }

  /* Header */
  .header {
    display: flex; justify-content: space-between; align-items: center;
    gap: 16px; flex-wrap: wrap; margin-bottom: 20px;
  }
  .brand { display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1 1 auto; }
  .brand-mark {
    flex: 0 0 auto; width: 42px; height: 42px; border-radius: 12px;
    display: grid; place-items: center;
    color: #fff; font-size: 14px; font-weight: 700; letter-spacing: .04em;
    background: linear-gradient(135deg, #6366f1, #8b5cf6 58%, #22d3ee);
    box-shadow: 0 8px 18px rgba(79, 70, 229, .28), inset 0 1px 0 rgba(255, 255, 255, .28);
  }
  .brand-text { min-width: 0; flex: 1 1 auto; }
  .meta {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    margin-top: 2px; color: var(--muted); font-size: 12px;
    max-width: 100%; overflow-wrap: anywhere;
  }
  .meta-sep { width: 3px; height: 3px; border-radius: 50%; background: var(--border-strong); }
  .health { display: inline-flex; align-items: center; }
  .status-dot {
    display: inline-block; width: 8px; height: 8px; border-radius: 50%;
    background: var(--muted); margin-right: 6px; vertical-align: middle;
    box-shadow: 0 0 0 3px var(--border);
    transition: background var(--dur) var(--ease), box-shadow var(--dur) var(--ease);
  }
  .status-dot.on { background: var(--ok); box-shadow: 0 0 0 3px var(--ok-bg); }
  .header-actions { flex-wrap: nowrap; }

  /* Segmented tabs */
  .tabs {
    display: flex; align-items: center; gap: 4px; flex-wrap: wrap;
    width: fit-content; max-width: 100%;
    margin: 0 0 20px; padding: 4px;
    background: var(--card); border: 1px solid var(--border);
    border-radius: 12px; box-shadow: var(--shadow-sm);
  }
  .tabs button {
    border: 0; background: transparent; box-shadow: none;
    color: var(--muted); font-size: 13px; font-weight: 500;
    padding: 7px 14px; min-height: 32px; border-radius: 8px;
  }
  .tabs button:hover { background: var(--accent-soft); color: var(--accent-text); }
  .tabs button:active { transform: none; }
  .tabs button:focus-visible { box-shadow: 0 0 0 3px var(--ring); }
  .tabs button.active {
    background: var(--accent-btn); color: #fff; font-weight: 600;
    box-shadow: 0 1px 2px rgba(16, 24, 40, .18), inset 0 1px 0 rgba(255, 255, 255, .16);
  }

  /* Buttons */
  button {
    cursor: pointer; font: inherit; font-size: 13px; font-weight: 550; line-height: 1.4;
    min-height: 34px; padding: 7px 14px;
    color: var(--text); background: var(--card-2);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    box-shadow: var(--shadow-sm);
    transition: background var(--dur) var(--ease), border-color var(--dur) var(--ease),
      color var(--dur) var(--ease), box-shadow var(--dur) var(--ease), transform var(--dur) var(--ease);
  }
  button:hover { background: var(--bg-soft); border-color: var(--border-strong); }
  button:active { transform: translateY(1px); }
  button:focus-visible { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--ring); }
  button.primary {
    color: #fff; border-color: transparent;
    background: linear-gradient(180deg, var(--accent-btn), var(--accent-btn-hover));
    box-shadow: 0 1px 2px rgba(16, 24, 40, .18), inset 0 1px 0 rgba(255, 255, 255, .16);
  }
  button.primary:hover { filter: brightness(1.07); background: linear-gradient(180deg, var(--accent-btn), var(--accent-btn-hover)); }
  button.ghost { background: transparent; border-color: transparent; box-shadow: none; color: var(--muted); }
  button.ghost:hover { background: var(--accent-soft); color: var(--accent-text); border-color: transparent; }
  button.destructive { color: var(--err); background: var(--err-bg); border-color: rgba(220, 38, 38, .28); box-shadow: none; }
  button.destructive:hover { background: rgba(220, 38, 38, .18); border-color: rgba(220, 38, 38, .45); }
  button.small { min-height: 28px; padding: 4px 10px; font-size: 12px; border-radius: 8px; }
  button:disabled { opacity: .55; cursor: progress; transform: none; }

  /* Forms */
  input, select, textarea {
    width: 100%; min-height: 36px; padding: 8px 11px;
    color: var(--text); background: var(--card-2);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    font: inherit; font-size: 13px;
    transition: border-color var(--dur) var(--ease), box-shadow var(--dur) var(--ease), background var(--dur) var(--ease);
  }
  input:hover, select:hover, textarea:hover { border-color: var(--border-strong); }
  input:focus, select:focus, textarea:focus {
    outline: none; border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--ring); background: var(--card);
  }
  input::placeholder, textarea::placeholder { color: var(--muted); opacity: .72; }
  label { display: block; font-size: 12px; font-weight: 550; color: var(--muted); margin: 12px 0 5px; }
  h3 + label, h3 + div + label { margin-top: 8px; }
  .hint { font-size: 12px; color: var(--muted); line-height: 1.6; margin-top: 8px; }
  .field-hint { font-size: 11.5px; color: var(--muted); line-height: 1.5; margin-top: 4px; }
  .auto-width { width: auto; min-width: 132px; }
  .stack { margin-top: 16px; }
  .stack:empty { display: none; }
  #checkinStatusBox:not(:empty) {
    margin-top: 20px; padding-top: 12px; border-top: 1px solid var(--border);
  }
  #checkinStatusBox:not(:empty)::before {
    content: "签到状态"; display: block;
    font-size: 11px; font-weight: 600; letter-spacing: .04em;
    color: var(--muted); margin-bottom: 2px;
  }
  .log-footer { justify-content: space-between; margin-top: 16px; }

  /* Layout + cards */
  .grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 340px), 1fr)); }
  .grid.two { grid-template-columns: repeat(auto-fit, minmax(min(100%, 380px), 1fr)); }
  .grid.overview { grid-template-columns: minmax(0, 1.12fr) minmax(0, .88fr); align-items: start; }
  .grid.overview-lower { grid-template-columns: minmax(0, 1.2fr) minmax(0, .8fr); align-items: start; margin-top: 16px; }
  .grid.account { grid-template-columns: minmax(0, 1.55fr) minmax(0, 1fr); align-items: start; }
  .card {
    background: var(--card); border: 1px solid var(--border);
    border-radius: var(--radius-lg); padding: 20px; box-shadow: var(--shadow-sm);
    min-width: 0;
  }
  .card-head {
    display: flex; justify-content: space-between; align-items: center;
    gap: 12px; flex-wrap: wrap;
    margin: 0 0 16px; padding-bottom: 12px; border-bottom: 1px solid var(--border);
  }
  .card-head h2 { margin: 0; padding: 0; border: 0; }
  .card-head .muted { font-weight: 400; }
  .card-note { font-size: 12px; color: var(--muted); margin-top: 2px; }
  .card-desc { margin: 0 0 8px; color: var(--muted); font-size: 12.5px; line-height: 1.6; }
  .page { display: none; }
  .page.active { display: block; animation: page-in 180ms var(--ease); }
  @keyframes page-in { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: none; } }

  /* Metrics */
  .metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(128px, 1fr)); gap: 12px; }
  .metrics-block { margin: 4px 0 20px; }
  .metric {
    position: relative; overflow: hidden; min-width: 0;
    padding: 14px 14px 12px;
    background: linear-gradient(145deg, var(--accent-soft), var(--card-2) 78%);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .metric::after {
    content: ""; position: absolute; right: -24px; bottom: -36px;
    width: 96px; height: 96px; border-radius: 50%;
    background: radial-gradient(circle, var(--chart-soft), transparent 68%);
    pointer-events: none;
  }
  .metric .label { position: relative; font-size: 12px; font-weight: 550; color: var(--muted); }
  .metric .value {
    position: relative; margin-top: 4px;
    font-size: 22px; font-weight: 700; line-height: 1.25;
    letter-spacing: -.02em; font-variant-numeric: tabular-nums;
  }
  .metric .value .ok, .metric .value .warn, .metric .value .err {
    display: inline-flex; align-items: center; padding: 1px 10px;
    font-size: 13px; font-weight: 600; border-radius: 999px;
  }
  .metric .value .ok { background: var(--ok-bg); }
  .metric .value .warn { background: var(--warn-bg); }
  .metric .sub { position: relative; min-height: 18px; margin-top: 2px; font-size: 11.5px; color: var(--muted); }

  /* Stats */
  .stat {
    display: flex; justify-content: space-between; align-items: baseline;
    gap: 16px; padding: 8px 0; border-bottom: 1px dashed var(--border); font-size: 13px;
  }
  .stat:last-child { border-bottom: none; }
  .stat > span:last-child { text-align: right; word-break: break-word; }

  /* Pills */
  .pill {
    display: inline-flex; align-items: center; gap: 5px; white-space: nowrap;
    margin: 0 4px 2px 0; padding: 1px 9px;
    font-size: 11px; font-weight: 550; line-height: 1.7;
    color: var(--muted); background: var(--bg-soft);
    border: 1px solid var(--border); border-radius: 999px;
  }
  .pill.ok, .pill.warn, .pill.err { border-color: transparent; }
  .pill.ok { color: var(--ok); background: var(--ok-bg); }
  .pill.warn { color: var(--warn); background: var(--warn-bg); }
  .pill.err { color: var(--err); background: var(--err-bg); }
  .pill.ok::before, .pill.warn::before, .pill.err::before {
    content: ""; width: 5px; height: 5px; border-radius: 50%; background: currentColor;
  }
  .pill.spend { border-color: transparent; color: var(--accent-text); background: var(--accent-soft); }
  .pill.recharge { border-color: transparent; color: var(--ok); background: var(--ok-bg); }
  .pill.spend::before, .pill.recharge::before {
    content: ""; width: 5px; height: 5px; border-radius: 50%; background: currentColor;
  }

  /* Usage log table: content-sized columns keep badges on one line; the freed
     width goes to 详情 instead of leaving a gap before Tokens. */
  table.logs-table { table-layout: auto; min-width: 0; }
  table.logs-table th, table.logs-table td { padding: 10px 12px; }
  table.logs-table tbody tr { transition: background var(--dur) var(--ease); }
  table.logs-table tbody tr:hover { background: var(--bg-soft); }
  table.logs-table .log-time {
    width: 1%; min-width: 150px; white-space: nowrap;
    color: var(--muted); font-variant-numeric: tabular-nums;
  }
  table.logs-table .log-model { width: 1%; white-space: nowrap; font-weight: 550; }
  table.logs-table .log-model > span {
    display: block; max-width: 150px; overflow: hidden;
    white-space: nowrap; text-overflow: ellipsis;
  }
  table.logs-table .log-type { width: 1%; white-space: nowrap; }
  table.logs-table .log-type .pill { margin: 0; }
  table.logs-table .log-num {
    width: 1%; text-align: right; white-space: nowrap;
    color: var(--muted); font-variant-numeric: tabular-nums;
  }
  table.logs-table .log-cost {
    width: 1%; text-align: right; white-space: nowrap; font-variant-numeric: tabular-nums;
  }
  table.logs-table .log-cost.zero { color: var(--muted); }
  table.logs-table .log-detail { width: 100%; }
  table.logs-table .log-detail > span {
    display: block; max-width: 100%; overflow: hidden; white-space: nowrap;
    text-overflow: ellipsis; color: var(--muted);
  }
  table.logs-table th.right { text-align: right; }

  /* Banners + empty states */
  .banner {
    padding: 12px 14px; font-size: 13px; line-height: 1.6;
    background: var(--accent-soft); border: 1px solid var(--border);
    border-left: 3px solid var(--accent); border-radius: 10px;
  }
  .banner.error { background: var(--err-bg); border-color: rgba(220, 38, 38, .25); border-left-color: var(--err); }
  .banner.warn { background: var(--warn-bg); border-color: rgba(217, 119, 6, .25); border-left-color: var(--warn); }
  .empty { padding: 36px 16px; text-align: center; color: var(--muted); font-size: 13px; line-height: 1.6; }
  .empty::before {
    content: ""; display: block; width: 30px; height: 30px; margin: 0 auto 10px;
    border-radius: 50%; border: 2px dashed var(--border-strong);
  }

  /* Tables */
  .scroll {
    max-height: 440px; overflow: auto; overscroll-behavior: contain;
    scrollbar-width: thin; scrollbar-color: var(--border-strong) transparent;
  }
  table { width: 100%; border-collapse: separate; border-spacing: 0; font-size: 13px; }
  th, td { text-align: left; padding: 9px 12px; border-bottom: 1px solid var(--border); vertical-align: middle; }
  thead th {
    position: sticky; top: 0; z-index: 1;
    padding: 8px 12px; font-size: 11px; font-weight: 600;
    color: var(--muted); background: var(--card);
    border-bottom: 1px solid var(--border-strong);
  }
  tbody tr { transition: background var(--dur) var(--ease); }
  tbody tr:hover { background: var(--hover); }
  tbody tr:last-child td { border-bottom: none; }
  td .muted { font-size: 11.5px; }
  .menu-item { cursor: pointer; }
  .menu-item:hover td { background: var(--accent-soft); }

  /* Charts */
  .charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr)); gap: 16px; }
  .chart-panel {
    padding: 14px 14px 10px;
    background: var(--card-2); border: 1px solid var(--border); border-radius: var(--radius);
  }
  .chart-title { font-size: 12px; font-weight: 600; color: var(--muted); margin-bottom: 12px; }
  .bars {
    position: relative; display: flex; align-items: flex-end; gap: 3px;
    height: 150px; padding: 8px 4px 0;
    border-bottom: 1px solid var(--border-strong);
    background-image: repeating-linear-gradient(to top, var(--grid-line) 0 1px, transparent 1px 25%);
  }
  .bars .bar {
    position: relative; flex: 1 1 0; min-width: 2px;
    background: linear-gradient(180deg, var(--chart), var(--chart-strong));
    border-radius: 4px 4px 2px 2px; opacity: .9;
    transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease), filter var(--dur) var(--ease);
  }
  .bars .bar:hover { opacity: 1; transform: translateY(-1px); filter: saturate(1.12); }
  .bars .bar span {
    display: none; position: absolute; bottom: calc(100% + 8px); left: 50%;
    transform: translateX(-50%);
    padding: 5px 9px; white-space: nowrap;
    font-size: 11px; font-weight: 550; line-height: 1.4;
    color: var(--tooltip-text); background: var(--tooltip-bg);
    border-radius: 7px; box-shadow: var(--shadow-md);
    pointer-events: none; z-index: 3;
  }
  .bars .bar span::after {
    content: ""; position: absolute; top: 100%; left: 50%; transform: translateX(-50%);
    border: 4px solid transparent; border-top-color: var(--tooltip-bg);
  }
  .bars .bar:hover span { display: block; }
  .bars > .empty { width: 100%; align-self: center; padding: 16px; }
  .bars > .empty::before { display: none; }
  .legend {
    display: flex; align-items: center; gap: 14px; flex-wrap: wrap;
    margin-top: 10px; font-size: 11.5px; color: var(--muted);
  }
  .legend span { display: inline-flex; align-items: center; }
  .legend i { width: 9px; height: 9px; border-radius: 3px; background: var(--chart); margin-right: 6px; }
  .legend .legend-hint { margin-left: auto; }

  /* Account page split */
  .split { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 24px; }
  .split > div { min-width: 0; }
  .split h3:first-child { margin-top: 0; }
  .split h3 + label { margin-top: 12px; }
  .split > div + div { border-left: 1px solid var(--border); padding-left: 24px; }
  @media (max-width: 760px) {
    .split { grid-template-columns: minmax(0, 1fr); gap: 8px; }
    .split > div + div { border-left: 0; padding-left: 0; padding-top: 16px; border-top: 1px solid var(--border); }
  }

  /* Misc */
  .kv { display: grid; grid-template-columns: auto 1fr; gap: 4px 12px; font-size: 13px; }
  .kv span:nth-child(odd) { color: var(--muted); }
  code {
    padding: 1px 5px; font-size: 11.5px;
    font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    background: var(--bg-soft); border: 1px solid var(--border); border-radius: 5px;
  }
  pre {
    white-space: pre-wrap; word-break: break-all; margin: 8px 0 0;
    max-height: 260px; overflow: auto; padding: 10px 12px;
    font-size: 12px; background: var(--bg-soft); border: 1px solid var(--border); border-radius: 10px;
  }
  .toast {
    position: fixed; right: 24px; bottom: 24px; z-index: 20;
    max-width: 360px; padding: 12px 16px;
    font-size: 13px; color: var(--text); background: var(--card);
    border: 1px solid var(--border); border-left: 3px solid var(--ok);
    border-radius: 12px; box-shadow: var(--shadow-md);
    opacity: 0; transform: translateY(10px) scale(.98); pointer-events: none;
    transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease);
  }
  .toast.show { opacity: 1; transform: none; }
  .toast.err { border-left-color: var(--err); color: var(--err); }

  /* Skeletons */
  .skeleton-stack { padding: 6px 0; }
  .skeleton {
    position: relative; overflow: hidden; background: var(--border);
    border-radius: 6px;
  }
  .skeleton::after {
    content: ""; position: absolute; inset: 0; transform: translateX(-100%);
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, .55), transparent);
    animation: shimmer 1.4s infinite;
  }
  @keyframes shimmer { to { transform: translateX(100%); } }
  .skeleton-line { height: 12px; margin: 10px 0; }
  .skeleton-line.short { width: 58%; }
  .skeleton-line.medium { width: 78%; }
  .skeleton-line.tall { height: 18px; }
  @media (prefers-color-scheme: dark) {
    .skeleton::after { background: linear-gradient(90deg, transparent, rgba(255, 255, 255, .08), transparent); }
  }

  /* Scrollbars */
  ::-webkit-scrollbar { width: 10px; height: 10px; }
  ::-webkit-scrollbar-thumb {
    background: var(--border-strong); border-radius: 999px;
    border: 2px solid var(--card);
  }
  ::-webkit-scrollbar-track { background: transparent; }

  @media (max-width: 980px) {
    .grid.overview, .grid.overview-lower, .grid.account { grid-template-columns: minmax(0, 1fr); }
  }
  @media (max-width: 720px) {
    .app { padding: 20px 14px 40px; }
    .tabs { width: 100%; }
    .tabs button { flex: 1 1 auto; }
    .header-actions { width: 100%; }
    .header-actions button { flex: 1; }
    .card { padding: 16px; }
    .card-head { align-items: flex-start; }
  }
  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after {
      animation-duration: .001ms !important;
      animation-iteration-count: 1 !important;
      transition-duration: .001ms !important;
      scroll-behavior: auto !important;
    }
  }
`
