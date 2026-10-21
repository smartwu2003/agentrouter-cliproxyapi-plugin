# AgentRouter plugin for EasyCLIProxyAPI / CLIProxyAPI

把 [AgentRouter](https://agentrouter.org) 公益站接入 CLIProxyAPI（EasyCLIProxyAPI 内核）。插件以原生动态库形式运行，提供：

- **模型目录**：启动时用 API Key 从 AgentRouter 拉取当前可用模型，并按模型自动选择上游协议（Chat Completions / Responses / Anthropic Messages）。
- **执行器**：把客户端请求翻译成 AgentRouter 需要的协议，透传工具调用、图片输入、思考内容与流式增量；响应翻译回客户端协议（OpenAI Chat、OpenAI Responses、Claude Messages 互转）。
- **User-Agent**：AgentRouter 会拒绝未识别的客户端。插件为每个上游请求附带可配置的客户端标识（默认 `claude-cli/2.1.161 (external, cli)`）。
- **视觉控制台**：EasyCLIProxyAPI 侧边栏的「AgentRouter」页面提供「概览 / 数据看板 / 模型 / 使用日志 / 账号与授权 / 设置」六个页签，把官网的余额、消耗趋势、模型列表、使用日志、签到与公告整合进来。
- **账号授权**：插件卡片上的「账号授权」按钮会打开本机授权页：可以用账号密码登录，也可以粘贴官网「个人设置 → 系统令牌」的访问令牌，或直接粘贴浏览器 `session` Cookie（GitHub/LinuxDO 登录的账号没有密码，用会话即可读取看板）。
- **可编辑配置**：页面里能直接改 User-Agent、API Key、代理、签到账号并保存到插件状态文件，不需要手改 YAML。
- **每日签到**：AgentRouter 在登录时发放当日奖励，因此签到的正确流程是「退出登录 → 重新登录」。插件按此流程实现，并支持定时自动签到（北京时间）。

## 适用范围与限制

- 插件编译时依赖 CLIProxyAPI SDK 的 C ABI。宿主与插件必须来自**同一代 SDK**（v8.0.x）。升级内核后请用 `make sync CORE_VERSION=vX.Y.Z` 重新编译。
- AgentRouter 的模型上下线很快，内置表只用于兜底；始终以 `/console/personal` 与管理页里的实时列表为准。
- **Claude 模型**既支持 Anthropic 原生协议，也支持 OpenAI 协议；**GPT-6 系列只支持 Responses**；DeepSeek/GLM 支持 OpenAI 与 Anthropic 两种协议。插件按模型自动选择，客户端用哪种协议进来都能得到对应协议的回复。
- 签到依赖账号密码（`username` / `password`）。GitHub/LinuxDO 登录的账号没有密码，可粘贴浏览器会话或系统令牌作为只读看板凭据，但「退出 → 登录」签到仍需要密码；站方若开启 Turnstile 人机验证，签到会失败并提示原因。
- AgentRouter 的控制台域名前有阿里云 WAF，脚本调用可能被随机拦截（返回 HTML 挑战页）。插件会自动延迟重试并切换到另一个域名；长期被拦截时在设置里配置 `proxy_url` 走代理访问 `https://agentrouter.org` 即可。

## 构建

需要 Go 1.26+ 与 C 编译器（Windows 用 MinGW-w64，Linux 用 gcc，macOS 用 Xcode CLT）。

```bash
cd plugin
# 对齐宿主 SDK 版本（EasyCLIProxyAPI 安装目录的 core-version.txt）
make sync CORE_VERSION=v8.0.36
make test
make build          # 产物：../dist/agentrouter_<os>_<arch>.<ext>
```

Windows 下也可以直接：

```powershell
.\build.ps1
```

产物是 `dist\agentrouter.dll`（Windows）、`agentrouter.dylib`（macOS）或 `agentrouter.so`（Linux）。

## 安装

1. 把动态库放进配置目录的插件目录，例如：
   ```
   <配置目录>/plugins/agentrouter.dll
   ```
   CLIProxyAPI 也会按 `plugins/<GOOS>/<GOARCH>/` 搜索。
2. 在 `config.yaml` 中打开插件并补齐配置：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    agentrouter:
      enabled: true
      priority: 10
      base_url: "https://ps.air-outer.com"     # 国内直连域名；海外/有代理时可用 https://agentrouter.org
      api_key: "sk-..."                        # https://ps.air-outer.com/console/token 创建
      user_agent: "claude-cli/2.1.161 (external, cli)"
      auto_discover_models: true
      username: "你的账号"                      # 签到用；也可以稍后在管理页临时填写
      password: "你的密码"
      checkin_on_startup: false
      checkin_hour: 9                          # 北京时间 09:00 后自动签到
      # 以下都可以留空，在插件页面的「设置 / 账号与授权」里填写，保存到插件状态文件：
      # dashboard_base_url: "https://ps.air-outer.com"  # 控制台域名（WAF 拦截时自动改用 agentrouter.org）
      # account_token: "..."                   # 系统令牌（个人设置页），配置后无需密码也能看看板
      # user_id: "123456"                      # New-Api-User，配合 account_token 使用
      # proxy_url: "http://127.0.0.1:7890"     # 仅用于控制面请求（模型列表/账号/签到）
      # model_protocols:
      #   deepseek-v4-flash: openai            # 强制某个模型走上游协议
      # models:                                # 手动指定模型（留空则完全依赖自动发现）
      #   - name: deepseek-v4-flash
      #     alias: fast
      #     protocols: [openai, anthropic]
      #     input_modalities: [text, image]      # 覆盖模型输入端能力（默认按内置表推断）
```

3. 重启内核（EasyCLIProxyAPI 里重启服务），确认 `GET /v0/management/plugins` 中 `registered` 与 `effective_enabled` 均为 `true`。
4. 管理页地址：`http://<host>:<port>/v0/resource/plugins/agentrouter/status`，EasyCLIProxyAPI 会自动在侧边栏生成入口。

## 界面与响应速度

- 控制台采用 1160px 居中布局、分段式页签、卡片标题分隔、语义化状态胶囊、骨架屏与 160ms 过渡（跟随系统深/浅色）。
- 页面数据按 90 秒 TTL 缓存（内存 + sessionStorage），切页先渲染缓存再后台刷新；手动刷新按钮发送 `?refresh=1` 跳过缓存。
- 后端对看板/日志/账号/公告/令牌接口做并行拉取 + TTL 缓存（15s/15s/20s/120s/30s）并合并并发相同请求；实测看板接口二次请求从 ~0.9s 降到 ~55ms。
- 登录、保存令牌/配置、签到后会清空相关缓存；页面隐藏时暂停看板轮询，返回时自动刷新一次。

## 账号授权（插件卡片按钮）

在 EasyCLIProxyAPI 的「插件」页，AgentRouter 卡片上会出现「账号授权」按钮。点击后内核会：

1. 在 `127.0.0.1` 的随机端口启动一次性授权页；
2. 打开浏览器，你在页面里登录 AgentRouter（账密 / 系统令牌 / 会话 Cookie 三种方式）；
3. 插件把拿到的访问令牌和用户 ID 写入插件状态文件，并自动复用或创建一个名为 `EasyCLIProxyAPI` 的无限额度 API 令牌作为推理密钥（若尚未配置 API Key）。

授权完成后侧边栏页面即可看到余额、历史消耗、请求次数、7/30/90 天消耗趋势、按模型统计、使用日志（消费/错误/签到分类）、API 令牌列表与签到状态。

## 客户端使用

统一使用 CLIProxyAPI 的地址和你在 `access.api-keys` 中配置的客户端密钥：

```bash
# OpenAI Chat
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer <client-key>" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}'

# OpenAI Responses（GPT-6 系列原生协议）
curl http://127.0.0.1:8317/v1/responses \
  -H "Authorization: Bearer <client-key>" -H "Content-Type: application/json" \
  -d '{"model":"gpt-6-astra","input":"hi"}'

# Claude Messages（Claude Code / ANTHROPIC_BASE_URL）
curl http://127.0.0.1:8317/v1/messages \
  -H "x-api-key: <client-key>" -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

Claude Code 这样配置（与代理一样走 EasyCLIProxyAPI）：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8317",
    "ANTHROPIC_AUTH_TOKEN": "<client-key>",
    "ANTHROPIC_MODEL": "deepseek-v4-flash"
  }
}
```

## 管理 API

以下路由位于 `/v0/management`，需要管理密钥（`Authorization: Bearer <management-key>`）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/agentrouter/refresh-models` | 用当前 API Key 拉取模型并刷新目录 |
| GET | `/agentrouter/account` | 账号信息（余额、分组、签到状态、可用模型） |
| POST | `/agentrouter/checkin` | 执行签到（退出→登录→对比余额） |
| POST | `/agentrouter/test` | 对指定模型发一条最小请求 |
| GET | `/agentrouter/models` | 当前发布的模型与协议 |
| GET | `/agentrouter/settings` | 脱敏后的插件设置 |

请求体可以携带 `api_key` / `username` / `password` 做**一次性**覆盖（管理页就是这么用的），这些值不会写盘、也不会被回显。

## 工作方式

- 请求路由：插件注册 `model_router`，只有 AgentRouter 目录中的模型才由插件执行，其他模型仍走内核内置 provider。
- 上游协议选择：`model_protocols` 显式覆盖 > 已知模型能力表 > 从模型名推断。多协议模型优先跟随客户端协议（例如 Claude Code 用 `/v1/messages` 进来时，DeepSeek 走 Anthropic 上游）。
- 协议转换使用 SDK 内置翻译器（`sdk/translator/builtin`）。Responses 上游在跨协议时以 Codex 语法作为中间格式，因为内置表只提供 Codex→Claude / Codex→OpenAI 的响应转换。
- 上游流量默认通过宿主 HTTP 客户端发出，便于请求日志与代理策略；模型目录、账号、签到这类控制面请求使用插件自己的客户端（可单独配置 `proxy_url`）。
- 签到逻辑：AgentRouter 基于 New API，每日奖励在登录流程内发放。插件先调用 `/api/user/logout`，再 `/api/user/login`，读取响应中的 `checked_in`，并用登录前后的余额差报告奖励金额。

## 图片输入

AgentRouter 的模型都支持图片输入（已用红方块/蓝圆点测试图实测）：

- `deepseek-v4-flash`、`gpt-6.1-sol`、Claude 系列均已验证可识别图片；
- `gpt-6-astra` 的 Responses 接口同样接受 `input_image`，但额度池紧张时会先返回 402。

插件会在模型列表里公开 `image` 输入能力，客户端（OpenAI `image_url`、Anthropic `image` 块）的图片会原样经内置翻译器送达上游；如需关闭，用 `input_modalities: [text]` 覆盖某个模型。

## 已知问题

- AgentRouter 的额度池是共享的，忙时 GPT/Claude 模型会返回 `402 Budget pool quota has been exhausted.`，这不是插件错误。
- 中文短提示词偶尔会触发站点的 `content-blocked` 风控（如“只回复OK”）；换英文或正常句子即可。
- 站点的 `/api/pricing` 会把 Responses-only 模型标注成 `openai`，插件对已知模型以内置能力表为准。

## 目录

```
plugin/            插件源码（Go，C ABI）
  main.go          生命周期与能力注册
  config.go        YAML 配置解析
  catalog.go       模型目录、协议判定、自动发现
  executor.go      执行器、协议转换、流式转发
  route.go         model_router 决策
  account.go       登录 / 签到 / 账号信息
  management.go    管理页与管理路由
  host.go          宿主回调（日志、流）
build.ps1           Windows 构建
build.sh            macOS / Linux 构建
```
