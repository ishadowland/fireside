# Lobster 接入(方式2: Hermes / OpenClaw backend driver)

> **Status**: 🚧 Draft v0.1 — 待用户审阅后冻结
> **Intent**: 让房间 agent 由外部 **Hermes Agent (Nous)** / **OpenClaw (Jigsaw)** 实例驱动,复用其完整工具栈、长时记忆与 MCP。与已落地的 `internal/agents` 服务端 hook(方式1)并存 —— 方式1 是「Fireside 自己调 OpenAI 兼容 API」,方式2 是「Fireside 把整段轮次外包给一个真 agent 进程」。
> **Codebase**: ❌ 尚未实现 —— 本设计为编码前规划(用户要求:先出计划 → 刷新文档 → 提 issue,不写代码)。
> **关系**: `docs/design/01-data-model.md` §4 `AgentLobster` / `LobsterAgentConfig`;`02-modules.md` §`internal/agents` `LobsterDriver`;`04-state-machines.md` §8 lobster 生命周期;需求 D20(信任模型)。

## 1. 背景:为什么需要方式2

`internal/agents`(方式1,commit `a0e1b7e`,issue #38)已经是可用闭环:
host 邀请 → 槽位(最多 2 个)→ 消息 hook → 调模型 → 回帖 + 广播。它把「连接方式」抽象成了 preset 的 `kind`(`openai` / `simple` / `openclaw`),并落到本地 gitignored 的 `agents.local.json`(写保护 API token)。

但方式1 本质上只是 **无状态 chat/completions 调用**:

- 没有 agent 自有记忆/工具/MCP —— 上下文全靠 Fireside 每次拼房的最近 `contextLimit`(10)条消息喂进去;
- `openclaw` kind 现在打的是 **legacy** `/api/chat`,既不是 OpenClaw 当前 gateway 的 OpenAI 兼容面,也没有会话持续性;
- 无法表达 ADR-0015(结构化澄清)、ADR-0017(长轮次进度)。

方式2 的目标:把**一个完整 agent 进程**(Hermes 或 OpenClaw gateway)作为 room 里一个参与者的后端。对话、记忆、工具调用都在 agent 侧发生,Fireside 只负责:

1. 把房间讨论串成一个 **perp-slot 会话**(session key 稳定映射 room+slot);
2. 转发新消息 → 等最终回复 → 落库 + 广播;
3. 可选地把 Fireside 自己产生的系统事件(房 ended、公告变更)注入会话。

这样 `LobsterDriver.Respond` 就是对 backend 的一次调用,`Driver` 接口(`02-modules.md`)天然映射。

## 2. 外部 backend 契约(已核实,2026-08-14)

两个后端都提供 **OpenAI 兼容 `/v1/chat/completions`**,这是整合的核心。Fireside 端统一走这一面,Hermes 与 OpenClaw 仅在后端「会话路由 header/字段」上略有差异,用 `backend_type` 区分。

### 2.1 OpenClaw Gateway(Jigsaw)

- 端点:`Gateway` 主端口 **HTTP+WS 多路复用**,OpenAI 兼容面**默认关**,需 `gateway.http.endpoints.chatCompletions.enabled: true`。
- `POST /v1/chat/completions`
  - 鉴权:`Authorization: Bearer <token>`(`gateway.auth.token` 或 `CLAWDBOT_GATEWAY_TOKEN`)。
  - **模型选择**:`model` 可取 `openclaw` / `openclaw/default`(稳定别名 → 默认 agent)/ `openclaw/<agent_id>`;`GET /v1/models` 会列出所有配置的 agent。
  - **会话路由**:请求带 OpenAI `user` 字段 → gateway 由此 derive **稳定 session key**。文档推荐的 App 用法:`user: "conv:<conversation_id>"`。另有显式 `x-openclaw-session-key`、兼容用的 `x-openclaw-agent-id` / `x-openclaw-model` / `x-openclaw-session` header。
  - 流式:`stream: true` → SSE。
- 旧版兼容:`/api/chat`(`{model, messages}` → `{reply}`)—— 即当前 preset `openclaw` kind 打的面,保留作 legacy。

### 2.2 Hermes Agent(Nous Research)

- 端点:`http://localhost:8642/v1`(默认;`API_SERVER_HOST`/`API_SERVER_PORT` 可配)。
- `POST /v1/chat/completions`
  - 鉴权:Bearer token(`API_SERVER_KEY`,缺失则 API key 鉴权整体关闭)。
  - 模型:`hermes-agent`(或 profile 名);`GET /v1/models` 列出。
  - **默认无状态**:整段对话由 `messages` 数组携带。
  - **会话持续**:`X-Hermes-Session-Id` header 续旧会话(从 state.db 载入历史,**需 API key 才开放**,并拒绝控制字符);`X-Hermes-Session-Key` 做长时记忆报 scope(如 Honcho)。
  - 流式:`stream: true` → SSE(`chat.completion.chunk` + `hermes.tool.progress`)。
- 健康检查:`GET /health`。

### 2.3 结论:统一调用面

| 维度 | OpenClaw | Hermes |
|---|---|---|
| 端点 | `<backend_url>/v1/chat/completions` | `<backend_url>/v1/chat/completions` |
| Bearer token | gateway token | `API_SERVER_KEY` |
| 模型名 | `openclaw/<agent_id>` 或 `openclaw/default` | `hermes-agent` / profile |
| 会话锚点 | `user: conv:<...>`(优先)或 `x-openclaw-session-key` | `X-Hermes-Session-Id` |
| 健康检查 | `GET /v1/models`(`/v1/chat/completions` 探测可选) | `GET /health` |

两者差异足够小,可以在「同一 OpenAI 兼容客户端」上按 `backend_type` 选会话字段即可,不需要两套 HTTP 客户端。

## 3. 数据模型与 preset 扩展

`01-data-model.md` 已有远期模型 `Agent.LobsterAgentConfig`,本次**不新建表**。改为扩展现有 preset(方式1 的 `Preset`)使其能描述「lobster 后端」,从而复用 `room_agents.agent_preset_id` + `PresetStore` 的全部安全语义(本地 0600 文件、write-only token、loopback-only 管理)。

```go
// Preset 扩展(方式1 结构上向后兼容):
const (
    ProviderOpenCLaw ProviderKind = "openclaw" // 保留 legacy /api/chat;加会话字段后指 gateway
    ProviderHermes   ProviderKind = "hermes"   // 新增 Hermes Agent(方式2)
)

// Preset 新增字段(可选):
type Preset struct {
    Kind ProviderKind `json:"kind"`
    // ...现有字段不动...

    // === 方式2 会话路由 ===
    AgentID    string `json:"agent_id,omitempty"`    // lobster 后端 agent 名:openclaw/<agent_id> 或 hermes profile
    SessionKey string `json:"session_key,omitempty"` // 会话前缀(默认 "conv");写进 user / X-Hermes-Session-Key
}
```

**设计点(需用户拍板)**:
- `SessionKey` 在 v0.1 可以做「preset 级固定」,但**同一 preset 进多个房间会共享同一 agent 会话** → 跨房间串记忆。倾向默认让 Fireside 用 `conv:<room_id>:<slot>` 自动生成,fixed 字段仅保留给调试。→ 需决策(§7 Q1)。
- `backend_type: "hermes" | "openclaw"`(设计文档 §4 字段)与 preset `kind` 重复。v0.1 以 `kind` 为唯一真源,`LobsterAgentConfig.backend_type` 尊重 legacy 文档但实现不读它。→ 需决策(§7 Q2)。

## 4. 驱动层:internal/agents/lobster_driver.go(新增)

映射 `02-modules.md` 的 `Driver` 接口到会话化 chat/completions:

```go
// LobsterDriver 把一个 (room, slot) 对应到一个 backend agent 会话。
type LobsterDriver struct {
    kind       ProviderKind      // hermes | openclaw
    baseURL    string            // preset.Endpoint()
    token      string            // preset.APIToken(write-only,不出 API)
    model      string            // preset.Model 或 "<backend>/<agent>"
    sessionKey string            // conv:<room_id>:<slot>(Fireside 生成)
    client     *http.Client      // 复用 config.httpTimeout 60s
}

// Respond:POST /v1/chat/completions
//  body:{model, user:"conv:...", messages:[{system},{user(主消息)},{assistant 历史}...]}
//  hermes 用 X-Hermes-Session-Id 头部承载会话锚点。
func (d *LobsterDriver) Respond(ctx context.Context, req RespondRequest) (Response, error)

// Ask / ResumeAfterAnswer:映射 ADR-0015 —— 后端 agent 输出结构化 question,
// Fireside 侧回调继续同一会话(同一 session key,追加 answer message)。
func (d *LobsterDriver) Ask(ctx context.Context, q Question) (QuestionID, error)
func (d *LobsterDriver) ResumeAfterAnswer(ctx context.Context, ans Answer) (Response, error)

// EmitProgress:ADR-0017 —— stream 模式下透传 hermes.tool.progress 事件。
func (d *LobsterDriver) EmitProgress(ctx context.Context, p Progress) error
```

**与方式1 的复用策略**:
- `chat()` 客户端(`service.go:979`)已按 `ProviderKind` 挑端点 + 解析响应 —— 新增 `ProviderHermes` branch,`providerEndpoint` 返回 `<base>/v1/chat/completions`(`simple` 语义),响应解析复用 `parseOpenAIResponse`(OpenClaw gateway 与 Hermes 都回标准 `choices[].message.content`)。
- 会话字段差异单独在「构建请求」处按 kind 处理:openclaw → body `user`;hermes → `X-Hermes-Session-Id` header。
- **原则上方式2 = 方式1 + (会话锚点 + backend agent 选择)**,因此可以做到小 diff:v0.1 先把「会话锚点」补进 `chat()`(对 `openclaw`/`hermes` kind 生效,`openai`/`simple` 不受影响),再让 Agent 管理器支持 `hermes` kind + `agent_id`。`LobsterDriver` 作为完整 `Driver` 接口实现留给 Sprint 2(ADR-0015/0017 一起落地)。

## 5. 会话生命周期与房间集成

按 `04-state-machines.md` §8 的时序,保持 Fireside 的「单播但无状态观看者」角色:

```
[Host 邀请 slot → preset(kind=hermes/openclaw)]
    ↓
[room_agents 记 agent_preset_id]
    ↓
[msg.send 落库 → messages hook 触发 TriggerRoom]
    ↓
[replyToRoom] resolveChatConfig → Config{Kind, BaseURL, APIKey, Model(sessionAgentID)}
    ↓
[chat()] POST /v1/chat/completions
    user: conv:<room_id>:<slot>              (openclaw)
    或  X-Hermes-Session-Id: <room_id>:<slot> (hermes)
    ↓
[parseOpenAIResponse → 取 choices[0].message.content]
    ↓
[CreateAgentMessage → hub 广播](不重触发,同方式1 无死循环)
```

关键语义决策:

- **同房会话稳定**:`conv:<room_id>:<slot>` 让 OpenClaw 侧该 slot 的 agent 在同一 agent session 里累积记忆(跨 Fireside 的 `contextLimit` 限制)。
- **房间间隔离**:session key 含 `room_id`,不同房间天然不同会话,不串记忆 —— 且对 OpenClaw reserve namespace(`subagent:`/`cron:`/`acp:`)安全。
- **房 ended**:Fireside 不做显式删除(Fireside 是纯观察者);后端 session 靠 gateway 自己的生命周期清理。可选:给 openclaw 会话打 `x-openclaw-message-channel: fireside/<room_id>` 便于策略过滤。(需开放问题确认是否要 map 一个 `X-Hermes-Session-Key` 的按房间 scope。)
- **主动模式 / free-speech**:复用方式1 完整机制(cooldown / mute / free-speech round),无需改动 —— 它们都在 `TriggerRoom` → `replyToRoom` 层,driven 的只是 `chat()` 的落点。

## 6. 安全(对齐 D20)

- **信任模型(MVP)**:Fireside 直连本机/内网 backend,Fireside 与 backend 之间用 Bearer token 双向确认;不引入额外网络隔离。Phase 2 再考虑 Linux namespace / sidecar。
- **token 安全**:沿用 preset write-only 契约 —— `PresetView.has_token` 只回 bool;`agents.local.json` 0600 + gitignored;loopback-only 管理面(`/dashboard/agents`,ADR-0019)。`x-openclaw-session-key` 若直接用,需拒绝 OpenClaw 保留前缀,校验 `^[a-zA-Z0-9:._-]{1,128}$`。
- **勿泄漏会话历史**:`X-Hermes-Session-Id` 续会话会加载历史,因此**必须**只在确认 backend 配了 `API_SERVER_KEY` 时才发送(后端无 key 时 rejection 已由 Hermes 侧保证,前向设 Fireside 校验「token 非空才发该 header」)。
- **错误弹回**:复用 `parseOpenAIResponse` 的 nil 守卫(#40)—— 上游 429/5xx 无 `error` 对象也不崩;错误文本进日志,不回给房间用户完整堆栈。

## 7. 开放问题(需拍板)

- **Q1 会话粒度**:`conv:<room_id>:<slot>` 由 Fireside 自动生成(房间间隔离、推荐)vs. preset 固定 `session_key`(可跨房共享记忆)。→ 推荐前者。
- **Q2 `kind` vs `backend_type` 谁为真源**:preset.kind(`openclaw`/`hermes`)为唯一真源,`Agent.LobsterAgentConfig.backend_type` 只算远期文档?→ 推荐前者。
- **Q3 是否需要完整 `LobsterDriver`(Sprint 2)**:v0.1 只在 `chat()` 加「会话锚点 + backend agent 选择」即可让方式2 跑通;ADR-0015/0017 的 `Ask`/`ResumeAfterAnswer`/`EmitProgress` 是否 v0.1 一并做?→ 推荐分开,先最小可用。
- **Q4 Hermes 会话历史**:Fireside 是否仍送 `contextLimit` 条当 `messages`(与后端会话叠加),还是依赖 `X-Hermes-Session-Id` 让后端 memory 兜底?v0.1 推荐前者(双保险、降首轮冷启动)。
- **Q5 端点可用性默认值**:preset 默认填 OpenClaw `https://localhost:18789` / Hermes `http://localhost:8642/v1` `base_url` 示例,要不要在 Agent 管理器加「kind=hermes/openclaw 时变化默认 endpoint」UX。

## 8. 落地计划(提案,未开始编码)

1. **P0 文档与 issue**:(本设计)锁定后提 GitHub issue,列 checkpoint。
2. **P1 最小接入**(小 diff):
   - `presets.go`:`ProviderHermes` + `AgentID`/`SessionKey` 字段 + `ValidProviderKinds` 更新 + 校验;
   - `service.go`:请求构建按 kind 加会话锚点(openclaw `user` / hermes header);`providerEndpoint` 处理 hermes → `/v1/chat/completions`;响应复用 `parseOpenAIResponse`;
   - 自检页(`check.js`)+ Agent 管理器 UI 支持 `hermes` kind。
   - 测试:`presets_test.go` / `service_test.go` 用 httptest 假 backend,断言请求体/header + 响应解析。
3. **P2 集成验证**:起一个本地 OpenClaw gateway(开 `chatCompletions.enabled`)与一个 Hermes(`API_SERVER_ENABLED`),跑 P1 后端端到端:邀请 → 发送 → 回帖落库广播 → free-speech round。
4. **P3(Sprint 2)** `LobsterDriver` 完整实现 + ADR-0015/0017(Ask/Resume/EmitProgress)+ lobster 生命周期 Shutdown hook。

## 9. References

- `docs/requirements/00-overview.md` D11(算力外部化)/ D20(信任模型)/ D29(announcement 注入顺序)
- `docs/design/01-data-model.md` §3-§4(`Participant.AgentConfig` / `AgentLobster` / `LobsterAgentConfig`)
- `docs/design/02-modules.md` §`internal/agents`(`Driver` 接口 + `LobsterDriver` 约定);§Q3 同步/异步决议
- `docs/design/04-state-machines.md` §8 lobster 生命周期
- `docs/requirements/03-decision-snapshot.md` 四、MCP 集成保留
- 外部契约:OpenClaw docs [gateway/openai-http-api](https://docs.openclaw.ai/gateway/openai-http-api);Hermes Agent docs [API Server](https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server)(2026-08-14 核)