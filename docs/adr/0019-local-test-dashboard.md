# ADR-0019: 本地测试 Dashboard(loopback-only、自动 stub 登录)

- **Status**: Accepted
- **Date**: 2026-07-31
- **Source**: 用户需求;docs/design/02-modules.md(模块布局);docs/design/03-protocol.md(WS 鉴权流程)

## Context

服务运行后需要一个**本地 web dashboard**,功能与安卓 App 对齐(WS 地址/Token 输入、连接、发 `auth.hello`、显示 `auth.welcome`/`auth.error`),用来在浏览器里快速测试基本功能,不依赖 Android 模拟器。要求:仅本地可用、不做登录功能。

约束与现状:

- WS 协议强制首帧 `auth.hello` 携带 JWT(ADR-0007),JWT 通过 `POST /v1/auth/login` 获得——所以「不做登录」不等于绕过鉴权,而是**由 dashboard 自动完成 stub 登录**,对用户无感。
- Sprint 0 用 stub code(`SMS_STUB_CODE`,默认 `1234`),无真实短信。dashboard 自动登录因此无需任何短信链路。
- 服务单端口 `:18080` 承载 REST + WS(ADR-0004),因此 dashboard 直接挂在主 HTTP 服务上,不另开端口。

## Decision

新增 `internal/dashboard` 包,通过 `go:embed` 内嵌静态页面(HTML+CSS+原生 JS,零前端构建链),挂载在 `/dashboard/`,并加 **loopback-only 中间件**:

- **访问控制**:仅接受回环地址(`127.0.0.1`/`::1`)的请求,其余 IP 一律 404。判定用 `net.SplitHostPort(c.Request.RemoteAddr)` 解析后的 `net.ParseIP(ip).IsLoopback()`,不信任 `X-Forwarded-For`(gin 默认 trusted proxies 校验会接受这些头,这里显式用 RemoteAddr 规避伪造)。
- **免登录 UX**:页面加载时 JS 自动 `POST /v1/auth/login {phone, code}`(phone 用固定测试号,code 由服务端 `GET /v1/dashboard/config` 下发 stub code,避免硬编码与 `SMS_STUB_CODE` 不一致),拿到 JWT 后展示在页面,用户点「Connect & Hello」连 WS。
- **功能对齐安卓 App**(Sprint 0 现状):WS 地址展示、JWT 展示、连接按钮、`auth.hello` 自动发送、状态面板(connecting/open/welcome/error/failure),外加一个事件日志区方便调试。
- **WS URL 自动推导**:浏览器 `location` 的 host + 固定路径 `/ws/v1/connect`,协议按页面协议切换 `http`→`ws`、`https`→`wss`。
- **不新增配置项**:无开关——dashboard 恒挂载,但 loopback-only 使其仅在开发机本地可达,天然不进生产网络。后续若需在受限内网开放,再引入 env 开关。

### 路由表

| Method | Path | 说明 |
|---|---|---|
| GET | `/dashboard/` | index.html |
| GET | `/dashboard/static/*` | app.js / style.css |
| GET | `/v1/dashboard/config` | `{stub_code}`(loopback-only) |

### 非目标 / 延后

- 不做房间/消息 UI(后端 Sprint 1 才有对应接口)。
- 不做 `wss` 下的自签证书处理(本地 dev 用 `ws://` 即可)。
- 不做多用户/会话管理(dashboard 是单机调试工具)。

## Consequences

- 浏览器打开 `http://localhost:18080/dashboard/` 即可完成「登录 → 连接 → hello → welcome」全链路自测,无需 Android 模拟器。
- loopback-only 判定独立于 gin 的 trusted proxies,避免 X-Forwarded-For 伪造绕过;代价是内网机器无法访问(符合需求)。
- 自动登录依赖 stub code 仍为 Sprint 0/1 现状;真实短信上线后 dashboard 的免登录 UX 需要重新评估(可能改为固定 dev 手机号白名单)。
