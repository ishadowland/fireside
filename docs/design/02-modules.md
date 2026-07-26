# Module Layout

> Code organization, package boundaries, dependency rules.
> 状态：🚧 Draft v0.1

## 总览

```
fireside/
├── cmd/
│   ├── server/             ← 主服务入口 (HTTP + WebSocket)
│   └── fsc/                ← CLI 工具 (fireside command family)
├── internal/
│   ├── domain/             ← 领域模型 + 业务规则 (无外部依赖)
│   ├── store/              ← 持久化层 (sqlc 生成的代码 + repository 包装)
│   ├── api/                ← HTTP handlers (Gin)
│   ├── ws/                 ← WebSocket hub + connection 管理
│   ├── routing/            ← 消息路由引擎 (@ 解析、agent 触发)
│   ├── agents/             ← Agent 适配器 (tool / custom / lobster)
│   ├── workspace/          ← 共享 MD 工作区 (go-git 内嵌)
│   ├── auth/               ← 手机号验证码 + JWT
│   ├── sms/                ← 短信网关 (Twilio / 阿里云)
│   ├── config/             ← 配置加载 (YAML)
│   └── logging/            ← slog wrapper
├── migrations/             ← golang-migrate SQL 文件
├── web/                    ← (未来) 静态资源 + 管理后台
├── deploy/
│   ├── systemd/
│   ├── nginx/
│   └── scripts/
├── docs/
├── README.md
└── go.mod
```

## Android 端

```
android/
├── app/
│   ├── src/main/java/com/firesidechat/app/
│   │   ├── MainActivity.kt
│   │   ├── ui/
│   │   │   ├── lobby/         ← 大厅界面
│   │   │   ├── room/          ← 房间界面
│   │   │   └── components/    ← 通用组件
│   │   ├── data/
│   │   │   ├── api/           ← Retrofit interface
│   │   │   ├── ws/            ← WebSocket client (OkHttp)
│   │   │   └── repo/          ← Repository 模式
│   │   ├── domain/
│   │   │   └── model/         ← Kotlin data classes
│   │   └── di/                ← Hilt modules
│   ├── build.gradle.kts
│   └── proguard-rules.pro
├── gradle/
└── README.md
```

## 分层规则(后端)

```
        ┌─────────────────────────┐
        │   cmd/server (入口)     │
        └───────────┬─────────────┘
                    │
        ┌───────────▼─────────────┐
        │   api / ws (传输层)     │   ← Gin + gorilla/websocket
        └───────────┬─────────────┘
                    │
        ┌───────────▼─────────────┐
        │   routing / agents      │   ← 业务规则层
        └───────────┬─────────────┘
                    │
        ┌───────────▼─────────────┐
        │   domain (纯领域模型)   │   ← 无外部依赖
        └───────────┬─────────────┘
                    │
        ┌───────────▼─────────────┐
        │   store / auth / sms    │   ← 基础设施
        └─────────────────────────┘
```

**依赖方向**:上→下,**下层绝不依赖上层**。
**`internal/`** 强制外部包无法引用,Go 编译器保证边界。

## 各模块职责

### `internal/domain`
- 定义 `User` / `Room` / `Participant` / `Message` / `Agent` 等 struct
- 定义**业务规则函数**(纯函数,无 IO):
  - `CanTransition(old, new RoomStatus) error`
  - `ExtractMentions(content) []string`
  - `IsHost(room, userID) bool`
- 100% 单元测试覆盖

### `internal/store`
- sqlc 生成的 `queries.sql.go`(类型安全的查询)
- 自定义 `Repository` 接口,把 sqlc 代码包装成业务接口
- 提供事务原语 `WithTx(ctx, fn func(tx Tx) error) error`
- **不依赖** domain 层以外的任何东西

### `internal/api` (HTTP)
- Gin router 配置
- REST endpoints(用于配置管理、归档查询等非实时操作):
  - `POST /v1/auth/login` — 发起验证码
  - `POST /v1/auth/verify` — 校验验证码,返回 JWT
  - `GET  /v1/rooms` — 当前用户的房间列表
  - `POST /v1/rooms` — 创建房间
  - `GET  /v1/rooms/:id` — 房间详情
  - `POST /v1/rooms/:id/end` — 结束房间
  - `GET  /v1/archives/:id` — 读归档
  - `GET  /v1/agents` — 服务端 agent 配置列表
  - `POST /v1/agents/:id/evoke` — 工具型 agent 调用
- **DTO ↔ domain model** 转换在此层完成
- 校验用 `go-playground/validator`

### `internal/ws` (WebSocket)
- `Hub` 管理所有活跃连接(房间 ID → 连接集合)
- `Connection` 封装单个客户端的读/写循环
- **消息泵**:
  - 客户端 → 服务端:解析 JSON envelope,dispatch 到 routing 层
  - 服务端 → 客户端:订阅 hub 的 channel
- **心跳**:每 30s ping/pong,60s 无响应踢线
- **重连**:客户端用 last_seen_msg_id 补齐遗漏消息(房间 active 时)

### `internal/routing`
- **这是 Fireside 的"业务大脑"**
- 收到新消息后的处理流水线:

```
[WS recv msg]
    ↓
[Parse: mentions, sender_kind, room_id]
    ↓
[Validate: sender is on_stage in room? mentions refer to valid participants?]
    ↓
[Persist: write to DB]
    ↓
[Broadcast: hub.fanout(room_id, msg)]
    ↓
[Trigger: for each mentioned participant → dispatch to agent OR mark unread for human]
    ↓
[Optional poll-trigger: wake up silent-mode active agents to decide if they should speak]
```

- **核心函数**:
  - `RouteMessage(ctx, msg) error`
  - `DispatchToAgent(ctx, agentID, roomCtx) error`
  - `ShouldAgentRespond(ctx, agent, recentMsgs) (bool, error)` — 给 active agent 的"是否插话"判断

### `internal/agents`
- `AgentDriver` interface:
  ```go
  type Driver interface {
      Kind() AgentType
      Respond(ctx context.Context, req RespondRequest) (Response, error)
  }
  ```
- 三个实现:
  - `ToolDriver` — 调 webhook
  - `CustomDriver` — 调 LLM API (OpenAI / Anthropic / MiniMax)
  - `LobsterDriver` — 调 Hermes/OpenClaw backend
- **`Driver` 是可注册的**(工厂模式),启动时根据 config 注册实例
- 配置从 `internal/store` 读

### `internal/workspace`
- **共享 MD 文档协作**(可选,房间挂载时启用)
- 基于 `go-git/go-git/v5` 内嵌库,不部署独立 git 服务
- 核心能力:
  - `WorkspaceService.Create(roomID)` — git init bare repo
  - `WorkspaceService.CreateBranch(workspaceID, agentID)` — 给每个 agent 独立 branch
  - `WorkspaceService.Commit(workspaceID, agentID, file, content)` — agent 提交
  - `WorkspaceService.Merge(workspaceID)` — 合并所有 unmerged branches
  - `WorkspaceService.GenerateSummary(workspaceID, agentID)` — 调用指定 custom agent 生成 diff 摘要
- 文件存储:`/var/fireside/workspaces/<room_id>/.git`
- **依赖**:`go-git` + `sergi/go-diff` + `yuin/goldmark`
- **不依赖** 任何外部 git CLI,纯 Go 实现
- **不依赖** server 侧 LLM API key(diff 摘要由 host 指定的 custom agent 生成,复用 `internal/agents` 的 driver)
- 智能定时触发:每个 workspace 一个 goroutine 监控"agent 静默 N 秒 + 至少 1 个 unmerged"条件
  - 人类聊天不影响计时(`participant.message` 事件不重置 workspace timer)
  - agent `workspace.commit` 事件重置 workspace timer

### `internal/auth`
- JWT 签发/校验(用 `golang-jwt/jwt/v5`)
- 验证码生成/校验
- **手机号格式校验**(E.164)
- token 通过 HTTP-only cookie 或 `Authorization: Bearer` 头传递

### `internal/sms`
- **接口设计**:
  ```go
  type Sender interface {
      Send(ctx context.Context, phone, code string) error
  }
  ```
- 实现:
  - `TwilioSender`
  - `AliyunSender`
  - `ConsoleSender`(dev 环境,直接打日志)
- 启动时按 config 选实现

### `internal/config`
- 用 `spf13/viper` 加载 `config.yaml`
- 配置项:
  ```yaml
  server:
    http_addr: "0.0.0.0:8080"
    ws_addr: "0.0.0.0:8081"   # 可与 http 同端口,用 path 区分
  database:
    dsn: "postgres://..."
  sms:
    provider: "aliyun"
    aliyun:
      access_key: "${ALIYUN_ACCESS_KEY}"
      secret: "${ALIYUN_SECRET}"
      sign_name: "Fireside"
      template_id: "SMS_xxx"
  jwt:
    secret: "${JWT_SECRET}"
    expiry_hours: 720  # 30 天
  agents:
    poll_interval_seconds: 60  # active agent 轮询间隔
    max_concurrent_responses: 5 # 同时响应的 agent 数上限
  ```
- 敏感字段从环境变量读(`.env` 文件)

## 关键数据流:用户发一条消息

```
[Android App]
    │ WebSocket send: {type:"msg.send", content:"@scribe 总结", mentions:["scribe"]}
    ▼
[ws.Connection → ws.Hub]
    │
    ▼
[routing.RouteMessage]
    │
    ├──► [store.InsertMessage]
    │
    ├──► [ws.Hub.Broadcast(room_id, msg)]
    │       │
    │       ▼
    │   [所有 on_stage 的连接收到 msg.created]
    │
    └──► [routing.TriggerAgent(scribe)]
            │
            ▼
        [agents.GetDriver(scribe)]
            │
            ▼
        [LobsterDriver.Respond(ctx, roomContext)]
            │
            ▼
        (异步) 返回结果 → [store.InsertMessage] → [Hub.Broadcast]
```

## 关键数据流:主持人拉 agent 进房间

```
[Android App]  POST /v1/rooms/{id}/participants {kind:"agent", agent_id:"scribe"}
    ▼
[api.Handler]
    │
    ▼
[domain.AddParticipant] (验证房间 active、主持人权限、未超 max)
    │
    ▼
[store.InsertParticipant]
    │
    ▼
[ws.Hub.BroadcastSystemMsg]  ("agent 'scribe' 上麦了")
    │
    ▼
[agents.LoadDriver(agent_id)]  ← 懒加载,首次拉入时启动
```

## 并发模型

- **WebSocket 连接** → 1 个 goroutine 读 + 1 个 goroutine 写(per connection)
- **房间 hub** → 每个房间一个 goroutine 监听 broadcast channel
- **Agent 响应** → 每个 active agent 一个 goroutine 监听 trigger channel
- **HTTP handlers** → 标准 Gin goroutine pool
- **DB 连接池** → `pgx` 默认 10 个连接,MVP 足够

## 测试策略

| 层 | 测试方式 |
|---|---|
| domain | 纯单元测试,无 mock |
| store | 集成测试,真 Postgres(可用 testcontainers) |
| api | handler 测试 + 真 Postgres |
| ws | 端到端:启动 test server,用 WebSocket client 发消息 |
| routing | 模拟 hub + agent driver,验证消息流 |
| agents | mock HTTP backend,验证驱动逻辑 |

---

## 待你审阅的开放问题

**Q1**: HTTP + WebSocket 单端口还是双端口?

**已决议(2026-07-26)**: **单端口**(Gin 路由 + gorilla/WS 共用 8080)
- Gin: `/ws/v1` → gorilla/WS;其他 → REST API
- Nginx: `location /ws/ { proxy_http_version 1.1; proxy_set_header Upgrade ...; proxy_read_timeout 3600s; }`
- 优势:Android 端只配 1 个 base URL,部署/证书/防火墙都简单
- 单端口实现需 Gin `readTimeout=0` 关闭 HTTP read timeout(WS 长连接)

**Q1 ✅** HTTP+WS 单端口

**Q2**: Android 端是否需要**离线消息缓存**(Room 关闭 App 后还能看历史)?

**已决议(2026-07-26)**: **做完整本地缓存 + 用户可控导出/清除**
- 存储方式:Android Room DB,每个房间一个表(分桶)
- 用户行为:
  - **退出登录 → 默认清空所有本地缓存**(防泄漏)
  - 设置里有"清除某个房间的本地缓存"按钮
  - 设置里有"导出某个房间为 JSON/Markdown"按钮
- 服务端房间 ended 后,**客户端仍可见已缓存**(直到用户主动清)
- **语义变化**:严格意义上不再"阅后即焚",但服务端视角仍保证
  - 服务端不持久
  - 用户主动导出 = 主动把消息带出 Fireside,这是用户的选择
  - 与"围炉鸿笺"调性自洽:用户决定纸笺的去留

### 数据模型(客户端 Room DB)
```
表 messages
  room_id, msg_id, sender_kind, sender_id, sender_name,
  content, content_type, mentions, reply_to_id, created_at

每房间独立表(分桶),房间退出/创建时自动建/清表
```

### 触发同步
- WebSocket `msg.created` 帧 → 写入本地
- App 启动时拉取 `GET /v1/rooms/<id>/messages?after=<last_msg_id>`(可选)
- 重连时用 session.resume + last_msg_id 补齐

**Q3**: Tool agent 的 webhook 是 **同步调用** 等返回再发消息,还是 **异步触发** (后台跑完再发)?
- 同步:用户立刻看到结果,但 WS 阻塞
- 异步:UX 好,但需要消息总线(我们刚才砍了 Redis)

回完这 3 个,我画下一篇 `03-protocol.md`。