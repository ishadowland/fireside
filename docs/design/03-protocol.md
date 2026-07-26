# WebSocket Protocol

> Message frame format, routing rules, error handling.
> 状态：🚧 Draft v0.1

## 连接层

### Endpoint

```
wss://<your-vps-domain>/ws/v1?token=<JWT>
```

- TLS 1.3 强制(由 Nginx 终结)
- JWT 放在 query string(简化移动端首次连接)
- 后续消息帧里也允许 `auth_token` 字段,**两端校验,任一缺失拒绝**

### 连接生命周期

```
[Client]                                   [Server]
   │  ─── WS upgrade (HTTP/1.1) ────────►  │
   │  ◄─── 101 Switching Protocols ──────  │
   │                                         │
   │  ─── hello frame ───────────────────►   │  (auth)
   │  ◄─── welcome frame ────────────────   │
   │                                         │
   │  ◄── ping (every 30s) ──────────────   │
   │  ─── pong ──────────────────────────►   │
   │                                         │
   │  ─── msg.send ─────────────────────►   │
   │  ◄── ack ──────────────────────────   │
   │  ◄── msg.created (broadcast) ──────   │
   │                                         │
   │  ─── close (or network drop) ──────►   │
   │                                         ▼
```

### 鉴权握手

客户端**第一个 frame** 必须是 `auth.hello`:

```json
{
  "type": "auth.hello",
  "token": "eyJhbGciOiJIUzI1...",
  "client_version": "1.0.0",
  "device_id": "android-uuid-xxx"
}
```

服务端校验 JWT,返回:

```json
// 成功
{
  "type": "auth.welcome",
  "user_id": "01HXY...",
  "display_name": "刘",
  "server_time": "2026-07-26T15:00:00Z"
}

// 失败
{
  "type": "auth.error",
  "code": "invalid_token",
  "message": "JWT expired or malformed"
}
```

**失败后服务端主动关闭连接**(close code 4401)。

### 心跳

- 服务端每 30s 发 `ping` 帧(JSON envelope `type: "system.ping"`)
- 客户端必须在 10s 内回 `pong`(`type: "system.pong"`)
- 60s 内无任何消息帧:服务端主动关闭(close code 4408)

## 消息帧 Schema

所有帧共用同一 envelope:

```typescript
interface Frame {
  type: string;                  // 必填,见下表
  id?: string;                   // 客户端生成,用于请求-响应关联
  ts?: number;                   // Unix ms,服务端填充(收到时刻)
  payload?: object;              // type-specific
  error?: ErrorPayload;          // 错误响应时填充
}

interface ErrorPayload {
  code: string;                  // 错误码(机器可读)
  message: string;               // 人类可读
  retryable: boolean;
}
```

### 完整帧类型目录

| 类型 | 方向 | 用途 |
|---|---|---|
| `auth.hello` | C→S | 握手 |
| `auth.welcome` | S→C | 握手成功 |
| `auth.error` | S→C | 握手失败 |
| `system.ping` | S→C | 心跳 |
| `system.pong` | C→S | 心跳响应 |
| `system.error` | S→C | 通用错误 |
| `msg.send` | C→S | 发新消息(仅人类) |
| `msg.created` | S→C | 新消息广播 |
| `msg.ack` | S→C | msg.send 的确认(已持久化) |
| `msg.typing` | 双向 | 输入状态(可选,Phase 2) |
| `room.create` | C→S | 创建房间(也可走 REST) |
| `room.created` | S→C | 房间创建确认 |
| `room.ended` | S→C | 房间结束广播 |
| `participant.join` | C→S | 上麦(也可由主持人代理) |
| `participant.joined` | S→C | 有人上麦广播 |
| `participant.leave` | C→S | 下麦 |
| `participant.left` | S→C | 有人下麦广播 |
| `participant.pull` | C→S | 主持人拉人/拉 agent |
| `hand.raise` | C→S | 大厅举手(也可走 REST) |
| `hand.update` | S→C | 举手状态变化(推送大厅里的所有人) |
| `agent.trigger` | C→S | 主动触发工具型 agent |
| `agent.respond` | S→C | agent 回复(走 msg.created 流程,但 sender_kind=agent) |
| `workspace.commit` | C→S | agent 提交 MD 文件变更 |
| `workspace.merge` | C→S | host 触发合并所有 unmerged branches |
| `workspace.state` | S→C | 工作区状态变化(branch 新增/commit/merge) |

## 关键帧详细格式

### 1. 发消息:`msg.send`

```json
{
  "type": "msg.send",
  "id": "client-uuid-1234",
  "payload": {
    "room_id": "01HXY...",
    "content": "@scribe 总结一下今天的讨论",
    "content_type": "text",
    "mentions": ["scribe"],          // 从 content 解析,客户端可预填
    "reply_to_id": "01HWZ..."        // 可选
  }
}
```

**服务端处理**:
1. 验证 sender 是 `room` 的 on_stage human
2. 验证 `mentions` 中的 ID 都是房间内存在的 participant
3. 持久化(分配 message ID + 服务端 timestamp)
4. 返回 `msg.ack`
5. 广播 `msg.created` 给所有 on_stage participants
6. 对每个 mentioned participant:
   - 如果是 human → 仅推送(客户端决定 UI 提示)
   - 如果是 agent → 触发 `routing.DispatchToAgent`
   - 如果是 active agent(即使没被 @)→ 加入"是否插话"决策队列

### 2. 消息广播:`msg.created`

```json
{
  "type": "msg.created",
  "ts": 1722000000000,
  "payload": {
    "msg": {
      "id": "01HXZ...",
      "room_id": "01HXY...",
      "sender": {
        "kind": "user",
        "id": "01HXY...user",
        "name": "刘"
      },
      "content": "@scribe 总结一下今天的讨论",
      "content_type": "text",
      "mentions": ["scribe"],
      "reply_to": null,
      "created_at": "2026-07-26T15:00:00Z"
    }
  }
}
```

**注意**:服务端广播给房间内所有 on_stage connections(无论 sender 是不是其中之一)。

### 3. 主持人拉人/拉 agent:`participant.pull`

```json
// 拉人类
{
  "type": "participant.pull",
  "id": "client-uuid-1235",
  "payload": {
    "room_id": "01HXY...",
    "target_kind": "user",
    "target_id": "01HXY...user",
    "stage_state": "on_stage"
  }
}

// 拉 agent
{
  "type": "participant.pull",
  "id": "client-uuid-1236",
  "payload": {
    "room_id": "01HXY...",
    "target_kind": "agent",
    "target_id": "scribe",
    "stage_state": "on_stage"
  }
}
```

**服务端处理**:
1. 验证 sender 是 host
2. 验证 room active
3. 验证未超 max_participants
4. 创建/更新 Participant
5. 广播 `participant.joined`
6. 对人类:推送系统消息提示对方
7. 对 agent:`agents.LoadDriver(agent_id)`

### 4. 举手:`hand.raise`

```json
{
  "type": "hand.raise",
  "id": "client-uuid-1237",
  "payload": {
    "note": "想讨论 X 话题"
  }
}
```

**服务端处理**:
1. 验证用户当前没有 pending hand
2. 创建 RaiseHand
3. 广播 `hand.update` 给**所有 active 房间的主持人**(大厅消息推送)
   - 注:大厅里的人类会看到弹窗,但不是严格意义的"实时大厅"
   - MVP 实现:服务端在 host connection 上推送
4. 同时写 REST 端的 `GET /v1/lobby/hands` 可查

### 5. Agent 回复(用 `msg.created` 同格式)

agent 输出的消息走**普通 `msg.created` 流程**,但 `sender.kind = "agent"`:

```json
{
  "type": "msg.created",
  "ts": 1722000060000,
  "payload": {
    "msg": {
      "id": "01HXZ...",
      "room_id": "01HXY...",
      "sender": {
        "kind": "agent",
        "id": "scribe",
        "name": "会议纪要员",
        "type": "lobster"
      },
      "content": "本次讨论要点:\n1. ...\n2. ...",
      "content_type": "text",
      "mentions": [],
      "created_at": "2026-07-26T15:01:00Z"
    }
  }
}
```

### 6. Agent 提交工作区变更:`workspace.commit`

仅当房间 `workspace_enabled=true` 且调用方是 on_stage agent 时合法。

```json
{
  "type": "workspace.commit",
  "id": "client-uuid-1240",
  "payload": {
    "room_id": "01HXY...",
    "file_path": "draft/section-1.md",
    "content": "# 需求澄清\n\n## 目标\n...",
    "commit_message": "初步草拟需求澄清章节"
  }
}
```

**服务端处理**:
1. 验证 sender 是 room 的 on_stage agent participant
2. 验证 `workspace_enabled=true`
3. 写入 agent 的 worktree
4. `go-git add` + `commit`(author = agent name)
5. 更新 `WorkspaceBranch.LastCommitSHA` + `HasUnmerged=true`
6. 广播 `workspace.state`

### 7. Host 触发合并:`workspace.merge`

仅 host 可触发。

```json
{
  "type": "workspace.merge",
  "id": "client-uuid-1241",
  "payload": {
    "room_id": "01HXY...",
    "summary_agent_id": "summarizer"   // 可选:覆盖 room.config.workspace_summary_agent_id
  }
}
```

**服务端处理**:
1. 验证 sender == host
2. 收集所有 `HasUnmerged=true` 的 branches
3. 串行 `go-git merge --no-ff` 到 main branch
4. 失败 → 标记 `HasConflicts=true`,记录冲突文件
5. 生成静态 diff stats(用 sergi/go-diff + goldmark)
6. **调用指定 custom agent 生成可读摘要**(复用 `internal/agents.Driver`)
   - 若 `summary_agent_id` 为空,使用 `room.config.workspace_summary_agent_id`
   - 若两者都为空,**只写静态 diff,不生成 AI 摘要**(降级行为)
7. 写 `WorkspaceMerge` 记录(含 `generated_by_agent_id`)
8. 重置所有 branches 的 `HasUnmerged=false`
9. 推一条 `msg.created`(system 类型)到房间,内容是 AI 摘要 + 静态统计
10. 广播 `workspace.state`

### 8. 工作区状态广播:`workspace.state`

```json
{
  "type": "workspace.state",
  "ts": 1722000000000,
  "payload": {
    "room_id": "01HXY...",
    "event": "branch_committed",     // branch_committed | merged | conflict
    "branch_name": "agent/scribe",
    "commit_sha": "abc1234",
    "merge_summary": {                // 仅 event=merged 时填充
      "total_files_changed": 3,
      "total_additions": 42,
      "total_deletions": 18,
      "merged_branches": ["agent/scribe", "agent/researcher"]
    }
  }
}
```

## 路由规则(决策表)

### 收到 `msg.send` 后

| 条件 | 动作 |
|---|---|
| sender 不是 host 且不是 on_stage human | reject `not_on_stage` |
| mentions 含非 participant ID | reject `invalid_mention` |
| 房间已 ended | reject `room_ended` |
| 通过校验 | persist + ack + broadcast + trigger |

### 收到 mention 一个 agent

| Agent Mode | Context Mode | 动作 |
|---|---|---|
| silent | any | 仅当 @ 才响应 |
| active | full | 立即响应,喂全量历史 |
| active | incremental | 立即响应,喂"自 on_stage 以来"的消息 |

### 收到 mention 一个 active agent 但无 @

(主动模式 agent 的"插话决策")

```
for each active agent in room, every N seconds:
    ctx = recent N messages + room context
    agent.ShouldRespond(ctx) → bool
    if true:
        dispatch as if @ mention
```

**频率控制**:每个 active agent **最多每 30s 触发 1 次**(防刷)。
**MVP 暂不实现定时轮询**,只在收到新消息后做一次决策(简化)。

## 错误码

| Code | 含义 | Retryable |
|---|---|---|
| `invalid_token` | JWT 无效 | false |
| `not_on_stage` | 发言者未上麦 | false |
| `invalid_mention` | @ 了不存在的 participant | false |
| `room_ended` | 房间已结束 | false |
| `room_full` | 房间已满 | false |
| `not_host` | 需要主持人权限 | false |
| `rate_limited` | 触发限流 | true |
| `internal` | 服务器内部错误 | true |

## 消息持久化与重连

### 客户端重连

```
[Client]                              [Server]
   │  ←── network drop ───              │
   │                                     │
   │  ─── WS reconnect ─────────────►    │
   │  ─── auth.hello ───────────────►    │
   │  ─── session.resume ───────────►    │  (含 last_msg_id)
   │  ◄── msg.missed (batch replay) ──   │
   │  ◄── msg.created (live from now) ─  │
```

`session.resume` 帧格式:

```json
{
  "type": "session.resume",
  "payload": {
    "last_msg_id": "01HXZ...",
    "subscribed_rooms": ["01HXY..."]
  }
}
```

服务端**不重发**已经下线的房间历史,只补齐"客户端下线期间产生的消息"。

### 历史分页(归档前查询)

```
GET /v1/rooms/{room_id}/messages?before={msg_id}&limit=50
```

- MVP 仅 host 可查
- 房间 ended 后此接口返回 410 Gone(消息已清)

## 限流

| 资源 | 限制 | 超限响应 |
|---|---|---|
| WebSocket 帧大小 | 64 KB | close 4409 |
| 每用户每分钟发送消息数 | 60 | `rate_limited` |
| 每房间每分钟消息数 | 300 | `rate_limited` |
| 单 agent 每小时响应数 | 100 | 不触发响应(静默丢弃) |

---

## 待你审阅的开放问题

**Q1**: WebSocket 鉴权用 **query string** 还是首个 frame?

**已决议(2026-07-26)**: **首个 frame**(`auth.hello`)
- 优势:JWT 不在 URL(server log 不会泄漏);重连清晰;鉴权失败可发 JSON 错误后 close
- Android 端:OkHttp `onOpen` 时发 hello 帧,`onMessage` 第一帧期望 auth.welcome/error
- 服务端鉴权失败 → 发 `auth.error` + close(4401)→ 客户端弹"重新登录"

**Q2**: active agent 的"插话决策"触发时机?

**已决议(2026-07-26)**: **新消息触发 + 定时轮询双触发**(都要)
- 双源触发都调同一个 `agent.ShouldRespond(ctx, recent_msgs)` 函数
- 定时参数:**每 1 分钟**检查一次(每个 active agent 一个 goroutine)
- 防刷:同一 agent 至少 20 秒内最多决策 1 次(无论哪种触发)
- 实现位置:`internal/routing/agent_decision.go`(单一决策入口)

**Q3**: Agent 长时间响应(超过 60s)在 UI 上怎么呈现?

**已决议(2026-07-26)**: **占位消息 + 删除-新建模式**(复用 02-Q3 tool agent 占位架构,扩展到所有 agent 类型)
- 流程:msg.send → 写占位 msg("⏳ <agent> 正在思考...")→ broadcast → ack → 后台 goroutine 调 driver(60s timeout)→ 成功后写新 msg + 软删占位
- 失败:写错误 msg + 软删占位
- 客户端时间轴:用户发言 → 占位"⏳" → 占位消失 → agent 完整回复
- 新增字段:`Message.IsPlaceholder`(bool),`Message.DeletedAt`(*time.Time)
- 新增帧:`msg.deleted`(payload: room_id, msg_id, reason)
- 不做流式 token(MVP 工作量大,异步场景收益小)

回完这 3 个,我画最后一篇 `04-state-machines.md`。