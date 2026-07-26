# State Machines

> Lifecycle diagrams for Room, Participant, Agent, and Archive.
> 状态：🚧 Draft v0.1

## 1. Room 状态机

```
       ┌──────────┐
       │ (init)   │
       └────┬─────┘
            │ host 创建房间
            ▼
       ┌──────────┐
   ┌──►│  active  │◄───────────┐
   │   └────┬─────┘            │
   │        │                  │
   │        │ host 调用 end    │ host 调用 transfer (新 owner)
   │        ▼                  │
   │   ┌──────────┐            │
   │   │  ending  │ (原子化,内部状态) │
   │   └────┬─────┘            │
   │        │                  │
   │        │ ArchiveOnEnd?    │
   │        ├─── no ───────────┼──► (持久) ended
   │        │ yes              │
   │        ▼                  │
   │   ┌────────────┐          │
   │   │ archiving  │          │
   │   │ (async)    │          │
   │   └────┬───────┘          │
   │        │ 归档完成          │
   │        ▼                  │
   │   ┌──────────┐            │
   └───┤  ended   │            │
       └──────────┘            │
```

### 状态说明

| 状态 | 持久化 | 允许操作 |
|---|---|---|
| `active` | DB | 收发消息、上下麦、拉人/拉 agent、设置 config |
| `ending` | 内存态 | 不接受新操作,正在做收尾 |
| `archiving` | DB(archived_at 填充中) | 不接受新消息,纪要 agent 在读历史 |
| `ended` | DB | 只读(GET 接口可见,POST 全 410) |

### 转换守卫

| From → To | 守卫 |
|---|---|
| `active → ending` | actor 必须 == host;room 必须 == active |
| `archiving → ended` | archive 记录已成功写入;messages 已清除 |
| `ending → ended`(no archive) | messages 立即清除 |

### 不可逆性

`ended` 是终态。**没有 restart / reopen**。
需要继续讨论 → 创建新房间(可保留同一个 host,引用原 room_id)。

## 2. Participant 状态机

### Human Participant

```
   ┌────────────────┐
   │ (不在房间)      │
   └───────┬────────┘
           │ host 调用 participant.pull(target_kind=user)
           │ 或 host 批准 hand.raise
           ▼
   ┌────────────────┐
   │  on_stage      │◄─────────┐
   └───────┬────────┘          │
           │                   │
           │ host 或 self 调 leave │ self 调 leave (任何人随时可下麦)
           ▼                   │
   ┌────────────────┐          │
   │  off_stage     │──────────┘
   │  (仍在房间但    │  (host 可再次 pull)
   │   不接收推送)    │
   └────────────────┘

   (退出房间) ← leave + 移除 participant 记录
```

### Agent Participant

```
   ┌────────────────┐
   │ (未加载)        │
   └───────┬────────┘
           │ host 调用 participant.pull(target_kind=agent)
           │ → agents.LoadDriver(agent_id)
           ▼
   ┌────────────────┐
   │  on_stage      │◄─────────┐
   │  + driver ready │          │
   └───────┬────────┘          │
           │                   │
           │ host 调用 leave   │
           ▼                   │
   ┌────────────────┐          │
   │  off_stage     │──────────┘
   │  driver kept   │
   └────────────────┘

   (房间 ended) ← driver.Shutdown() + 移除 participant 记录
```

**区别**:
- Agent 的 driver **首次上麦时懒加载**,off_stage 不卸载(避免反复初始化)
- 房间 ended 时统一 Shutdown 所有 driver

## 3. Agent Driver 生命周期

```
   ┌──────────────┐
   │ (unloaded)   │
   └──────┬───────┘
          │ LoadDriver()
          ▼
   ┌──────────────┐
   │  loading     │ ─── 失败 ──► error (回滚 participant.pull,提示用户)
   └──────┬───────┘
          │ 成功
          ▼
   ┌──────────────┐
   │  ready       │ ◄── Respond() ──► responding ──► ready
   └──────┬───────┘
          │ 房间 ended / server shutdown
          ▼
   ┌──────────────┐
   │  shutting    │
   │  down        │
   └──────┬───────┘
          ▼
       unloaded
```

### Driver 类型差异

| Type | LoadDriver 做什么 | Respond() 做什么 |
|---|---|---|
| tool | 仅校验 webhook URL | HTTP POST 给 handler,等结果(同步,timeout 30s) |
| custom | 读 config,初始化 LLM client | 调 LLM API(流式或非流式) |
| lobster | HTTP POST 给 backend `/sessions`,握手 | 调 backend `/respond`,流式收结果 |

## 4. RaiseHand 状态机

```
   ┌──────────────┐
   │ (不存在)      │
   └──────┬───────┘
          │ 用户 hand.raise
          ▼
   ┌──────────────┐
   │  pending     │ ◄── (用户取消) ──► cancelled
   └──────┬───────┘
          │
          ├── host hand.approve ──► approved + 创建 Participant(on_stage)
          │
          └── host hand.reject ──► rejected
```

### 守卫

| From → To | 守卫 |
|---|---|
| (无) → pending | 用户当前没有其他 pending;房间存在(房间必须是 host 主持) |
| pending → approved | actor == 房间 host |
| pending → rejected | actor == 房间 host |
| pending → cancelled | actor == 用户自己 |

### 边角情况

- 用户举手后断开连接:hand 保持 pending,不自动取消
- 主持人先 reject,后用户又 raise:允许(无时间限制)
- 主持人批准时房间已满:`approve` 失败,hand 保持 pending,提示 host

## 5. Archive 状态机

```
   ┌──────────────┐
   │ (无归档)      │
   └──────┬───────┘
          │ room 转入 archiving 状态
          ▼
   ┌──────────────┐
   │  drafting    │ ─── driver 失败 ──► failed
   └──────┬───────┘
          │ 成功写入 Archive 记录
          ▼
   ┌──────────────┐
   │  completed   │ → 清除 messages → room 转入 ended
   └──────────────┘
```

**关键不变量**:
- `completed` 后,原始 messages **必须清除**(防止泄漏)
- `failed` 后,房间状态保持 `archiving`,host 可手动重试或强制 end
- Archive 记录**与 room 解耦**,room 删除后 archive 仍可查

## 6. Workspace 生命周期

> 新增 2026-07-26 — 共享 MD 文档工作区

```
   ┌──────────────────┐
   │ (未创建)          │
   └────────┬─────────┘
            │ host 创建房间 + workspace_enabled=true
            ▼
   ┌──────────────────┐
   │  initialized     │ (git init bare repo, main branch 空)
   └────────┬─────────┘
            │ 第一个 agent 上麦并 commit
            ▼
   ┌──────────────────┐
   │  active          │ (有 branches + commits)
   └────────┬─────────┘
            │ host 触发 workspace.merge
            ▼
   ┌──────────────────┐
   │  merging         │ ─── 失败(冲突) ──► conflict (host 可重试)
   └────────┬─────────┘
            │ 成功
            ▼
   ┌──────────────────┐
   │  merged          │ (合并记录写入)
   └────────┬─────────┘
            │ 继续 agent commits
            ▼
        active (循环)

   ────────────────

   当 room ended:
        所有 workspace ──► archived (只读,与 archive 一起保存)
```

### 状态说明

| 状态 | 允许操作 |
|---|---|
| `initialized` | agent 上麦 + commit |
| `active` | agent 上麦 + commit + host 触发 merge |
| `merging` | 只读,等待合并结果 |
| `conflict` | host 可重试 merge / 强制跳过冲突文件 |
| `merged` | 自动回到 active(若有未合并 commits) |
| `archived` | 只读,跟随 room archive 永久保存 |

### 关键守卫

| From → To | 守卫 |
|---|---|
| (无) → initialized | 房间存在且 workspace_enabled=true |
| active → merging | actor == host;至少 1 个 branch HasUnmerged=true |
| merging → merged | 所有分支合并成功 |
| merging → conflict | 任一分支合并失败 |
| conflict → merging | host 显式重试 |
| 任意 → archived | room.ended |

### Branch 生命周期

- Branch 在 agent **首次 commit 时创建**(懒加载,不上麦不创建)
- Branch 在 agent **下麦时不删除**(重新上麦后可继续编辑)
- Branch 在 workspace **archived 时保留**(随 git repo 一起只读)

## 7. 用户登录态

```
   ┌──────────────┐
   │ (未登录)      │
   └──────┬───────┘
          │ 输入手机号 → POST /v1/auth/login
          ▼
   ┌──────────────┐
   │  awaiting_otp│ ─── 60s 内未验证 ──► (超时)
   └──────┬───────┘
          │ POST /v1/auth/verify (6 位数字)
          ├── 成功 ──► logged_in (返回 JWT)
          └── 失败 ──► (允许重试 5 次)
```

**MVP 简化**:
- 不存 session 表,JWT stateless
- "登录态"在客户端 = 持有有效 JWT
- 服务端不维护"在线用户列表"(WebSocket 连接自带)

## 8. 端到端时序:完整一次讨论

```
t=0   Host 创建房间 "周会-2026-07-26"
      → Room.active

t=10  Host 把 scribe (lobster agent) 拉进房间
      → Participant(agent=on_stage)
      → LobsterDriver loaded

t=20  Host 把 friend 拉进房间
      → Participant(human=on_stage)
      → friend WS 收到 participant.joined 通知

t=60  friend 发消息 "我们需要讨论 X"
      → Message 持久化
      → 所有 on_stage 收 msg.created
      → scribe (active agent) 决策:不插话 (无明确 @,话题刚启动)

t=120 Host 发 "@scribe 你怎么看"
      → Message 持久化
      → scribe 触发 Respond
      → scribe 调 LobsterDriver
      → 30s 后返回 "我的建议是..."
      → Message(sender=agent) 持久化 + 广播

t=180 Host 决定结束房间
      → Room.ending
      → ArchiveOnEnd=true
      → Room.archiving
      → 触发 scribe 以"纪要模式"读全量
      → Archive 写入(summary_md)
      → Messages 物理删除
      → Room.ended
      → 所有 Participant 删除
      → 所有 LobsterDriver.Shutdown()
```

---

## 状态转换的并发安全

| 资源 | 锁策略 |
|---|---|
| Room.Status | 乐观锁(`version` 列 + `WHERE version=?`) |
| Participant 上麦/下麦 | UNIQUE 约束 + 行锁 |
| Agent driver | 房间级 mutex(同一房间串行 driver 操作) |
| Message 持久化 | DB 事务,无应用层锁 |

## 待办(MVP 不实现,Phase 2+)

- Room.transfer host(技术可行,但 MVP 不开放)
- Participant.kick(主持人强制踢人 — 等价于 leave + 禁用 rejoin,MVP 简化)
- Agent.cooldown(防 agent 刷屏,MVP 仅限频,不智能)

---

## 待你审阅的开放问题

**Q1**: Room.ended 后,**新房间能否引用老房间 ID 作为"父话题"**?
- 关系:父子房间
- UI:历史房间列表里能展开看归档

**Q2**: hand.raise 超时(比如大厅停留超过 24h 无活动)是否自动清理?

**Q3**: Agent Driver 在房间 ended 时**立即 shutdown** 还是**延后 N 秒**(等正在响应的请求跑完)?

回完这 3 个,我把 4 篇设计文档一次性 commit + push,然后给你**编码前的最终冻结总结**。