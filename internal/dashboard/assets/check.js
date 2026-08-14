/* Fireside Dashboard — check.html (接口自检).
 *
 * End-to-end functional validation of every interface the server
 * currently supports, run from the browser against the live backend:
 *
 *   REST:  config / login / rooms(create,list,get) / join /
 *          messages(create,list) / end
 *   WS:    auth.hello -> welcome, room.subscribe -> subscribed,
 *          msg.send -> msg.created broadcast, room.ended broadcast
 *
 * Each check reports PASS/FAIL + duration; failures are logged with the
 * exact error. Checks run sequentially (a fresh room per run) so one
 * failure doesn't corrupt later state. ADR-0019 loopback-only,
 * ADR-0021 snake_case JSON.
 */
(() => {
  "use strict";

  const PHONE_A = "+8613800001111";
  const PHONE_B = "+8613800002222";

  const els = {
    runBtn: document.getElementById("run-btn"),
    summaryStatus: document.getElementById("summary-status"),
    summaryCount: document.getElementById("summary-count"),
    tbody: document.getElementById("checks-tbody"),
    log: document.getElementById("log"),
    aiBaseUrl: document.getElementById("ai-base-url"),
    aiApiKey: document.getElementById("ai-api-key"),
    aiModel: document.getElementById("ai-model"),
    aiSaveBtn: document.getElementById("ai-save-btn"),
    aiPingBtn: document.getElementById("ai-ping-btn"),
    aiStatus: document.getElementById("ai-status"),
  };

  const state = { code: "1234", tokA: null, tokB: null, roomId: null };
  const results = new Map(); // id -> {ok, ms}

  // ---- AI config UI ------------------------------------------------------
  const AI_KEY = "fireside.ai.config.v1"; // localStorage key

  function aiLoadLocal() {
    try { return JSON.parse(localStorage.getItem(AI_KEY) || "{}"); }
    catch { return {}; }
  }
  function aiSaveLocal(cfg) {
    try { localStorage.setItem(AI_KEY, JSON.stringify(cfg)); }
    catch { /* private mode */ }
  }

  async function aiLoadFromServer() {
    try {
      const cfg = await Fireside.jwtFetch("GET", "/v1/dashboard/ai-config", null);
      if (cfg && cfg.configured) {
        els.aiBaseUrl.value = cfg.base_url || "";
        els.aiModel.value = cfg.model || "";
      }
      const local = aiLoadLocal();
      if (local.api_key) els.aiApiKey.value = local.api_key;
      els.aiStatus.textContent = cfg && cfg.configured ? `已配置 ${cfg.model}` : "未配置";
      return cfg && cfg.configured;
    } catch (err) {
      log("err", `读取 AI 配置失败 — ${err.message}`);
      els.aiStatus.textContent = "读取失败";
      return false;
    }
  }

  async function aiSave() {
    const baseUrl = els.aiBaseUrl.value.trim();
    const apiKey = els.aiApiKey.value.trim();
    const model = els.aiModel.value.trim();
    if (!baseUrl || !model) {
      els.aiStatus.textContent = "端点 URL 与模型名必填";
      return false;
    }
    els.aiSaveBtn.disabled = true;
    try {
      const r = await Fireside.jwtFetch("POST", "/v1/dashboard/ai-config", null,
        { base_url: baseUrl, api_key: apiKey, model });
      aiSaveLocal({ api_key: apiKey });
      els.aiStatus.textContent = r.configured ? `已保存 ${model}` : "保存失败";
      log("ok", `AI 配置已保存: ${baseUrl} / ${model}`);
      return r.configured;
    } catch (err) {
      els.aiStatus.textContent = `保存失败: ${err.message}`;
      log("err", `保存 AI 配置失败 — ${err.message}`);
      return false;
    } finally {
      els.aiSaveBtn.disabled = false;
    }
  }

  async function aiPing() {
    const configured = await aiSave();
    if (!configured) return;
    els.aiPingBtn.disabled = true;
    els.aiStatus.textContent = "测试中…";
    try {
      const r = await Fireside.jwtFetch("POST", "/v1/dashboard/ai-ping", null);
      els.aiStatus.textContent = `连接正常 ${r.latency_ms}ms`;
      log("ok", `AI 端点连通，${r.latency_ms}ms`);
    } catch (err) {
      els.aiStatus.textContent = "连接失败";
      log("err", `AI ping 失败 — ${err.message}`);
    } finally {
      els.aiPingBtn.disabled = false;
    }
  }

  function log(level, msg) { Fireside.log(level, msg, els.log); }

  function require(cond, what) {
    if (!cond) throw new Error("前置失败: " + what);
  }

  function decodeUid(token) {
    try { return JSON.parse(atob(token.split(".")[1])).user_id || null; }
    catch { return null; }
  }

  function renderChecks() {
    if (!results.size) {
      els.tbody.innerHTML = `<tr><td colspan="5" class="empty">点击「运行全部检查」开始</td></tr>`;
      return;
    }
    els.tbody.innerHTML = CHECKS.map((c, i) => {
      const r = results.get(c.id);
      let pill = `<span class="status-pill">待运行</span>`;
      if (r) {
        if (r.skipped) {
          pill = `<span class="status-pill">SKIP</span>`;
        } else {
          pill = r.ok
            ? `<span class="status-pill status-active">PASS</span>`
            : `<span class="status-pill status-ended">FAIL</span>`;
        }
      }
      return `<tr>
        <td>${i + 1}</td>
        <td>${Fireside.escapeHtml(c.name)}</td>
        <td><code>${Fireside.escapeHtml(c.api)}</code></td>
        <td>${pill}</td>
        <td>${r ? r.ms + "ms" : "—"}</td>
      </tr>`;
    }).join("");
  }

  function updateSummary(done) {
    const pass = [...results.values()].filter((r) => r.ok && !r.skipped).length;
    const skip = [...results.values()].filter((r) => r.skipped).length;
    els.summaryCount.textContent = `${pass} / ${CHECKS.length}`;
    if (!done) {
      els.summaryStatus.textContent = "运行中…";
      return;
    }
    const fail = CHECKS.length - pass - skip;
    els.summaryStatus.textContent = fail === 0
      ? `全部通过 (${pass}/${CHECKS.length}${skip ? "，" + skip + " 跳过" : ""})`
      : `${fail} 项失败 (${pass}/${CHECKS.length}${skip ? "，" + skip + " 跳过" : ""})`;
    els.summaryStatus.className = "status " + (fail === 0 ? "ok" : "err");
  }

  async function runCheck(c) {
    const t0 = performance.now();
    let detail = "";
    try {
      detail = (await c.run()) || "";
      results.set(c.id, { ok: true, skipped: false, ms: Math.round(performance.now() - t0) });
      log("ok", `PASS  ${c.api}${detail ? " — " + detail : ""}`);
    } catch (err) {
      if (/^SKIP:/.test(err.message)) {
        results.set(c.id, { ok: false, skipped: true, ms: Math.round(performance.now() - t0) });
        log("out", `SKIP  ${c.api} — ${err.message.slice(5)}`);
      } else {
        results.set(c.id, { ok: false, skipped: false, ms: Math.round(performance.now() - t0) });
        log("err", `FAIL  ${c.api} — ${err.message}`);
      }
    }
    renderChecks();
  }

  async function runAll() {
    els.runBtn.disabled = true;
    try {
      for (const c of CHECKS) await runCheck(c);
    } finally {
      els.runBtn.disabled = false;
      updateSummary(true);
    }
  }

  // ---- WebSocket helpers --------------------------------------------------
  // Opens /ws/v1/connect, sends auth.hello, resolves after welcome with
  // { ws, user_id, next(type, opts) }. next() resolves the next frame of
  // `type` (optionally matching opts.match(frame)), consuming from a
  // buffer of frames received since welcome; rejects on timeout.
  function openWS(token) {
    return new Promise((resolve, reject) => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const ws = new WebSocket(`${proto}://${location.host}/ws/v1/connect`);
      const queue = [];
      const waiters = [];
      let welcomed = false;

      function nextFrame(type, opts) {
        opts = opts || {};
        const pred = opts.match || (() => true);
        const timeoutMs = opts.timeoutMs || 5000;
        return new Promise((res, rej) => {
          for (let i = 0; i < queue.length; i++) {
            if (queue[i].type === type && pred(queue[i])) {
              res(queue.splice(i, 1)[0]);
              return;
            }
          }
          const waiter = { res, rej, type, pred };
          waiters.push(waiter);
          setTimeout(() => {
            const i = waiters.indexOf(waiter);
            if (i >= 0) waiters.splice(i, 1);
            rej(new Error(`timeout (${timeoutMs}ms) waiting for ${type}`));
          }, timeoutMs);
        });
      }

      ws.onopen = () => ws.send(JSON.stringify({ type: "auth.hello", token }));
      ws.onmessage = (ev) => {
        let f;
        try { f = JSON.parse(ev.data); } catch { return; }
        if (!welcomed && f.type === "auth.welcome") {
          welcomed = true;
          resolve({ ws, user_id: f.user_id, next: nextFrame });
          return;
        }
        for (let i = 0; i < waiters.length; i++) {
          if (f.type === waiters[i].type && waiters[i].pred(f)) {
            waiters.splice(i, 1)[0].res(f);
            return;
          }
        }
        queue.push(f);
      };
      ws.onerror = () => { if (!welcomed) reject(new Error("WS error before welcome")); };
      ws.onclose = (ev) => { if (!welcomed) reject(new Error(`WS closed before welcome (${ev.code})`)); };
    });
  }

  function wsSend(ws, obj) { ws.send(JSON.stringify(obj)); }

  // ---- checks ---------------------------------------------------------------
  const CHECKS = [
    {
      id: "config",
      name: "配置读取 (stub code)",
      api: "GET /v1/dashboard/config",
      run: async () => {
        const cfg = await Fireside.jwtFetch("GET", "/v1/dashboard/config", null);
        if (!cfg || !cfg.stub_code) throw new Error("stub_code missing");
        state.code = cfg.stub_code;
        return `stub_code=${cfg.stub_code}`;
      },
    },
    {
      id: "login_a",
      name: "登录用户 A",
      api: "POST /v1/auth/login",
      run: async () => {
        const r = await Fireside.jwtFetch("POST", "/v1/auth/login", null,
          { phone: PHONE_A, code: state.code });
        if (!r.token) throw new Error("token missing");
        state.tokA = r.token;
        return `user_id=${decodeUid(r.token) || "?"}`;
      },
    },
    {
      id: "login_b",
      name: "登录用户 B",
      api: "POST /v1/auth/login",
      run: async () => {
        const r = await Fireside.jwtFetch("POST", "/v1/auth/login", null,
          { phone: PHONE_B, code: state.code });
        if (!r.token) throw new Error("token missing");
        state.tokB = r.token;
        return `user_id=${decodeUid(r.token) || "?"}`;
      },
    },
    {
      id: "create_room",
      name: "创建房间",
      api: "POST /v1/rooms",
      run: async () => {
        require(state.tokA, "A 未登录");
        const data = await Fireside.jwtFetch("POST", "/v1/rooms", state.tokA,
          { name: "接口自检", max_participants: 6 });
        const room = data.room || data;
        if (!room || !room.id) throw new Error("room.id missing");
        state.roomId = room.id;
        return `room_id=${room.id}`;
      },
    },
    {
      id: "list_rooms",
      name: "列出活跃房间",
      api: "GET /v1/rooms",
      run: async () => {
        require(state.roomId, "房间未创建");
        const data = await Fireside.jwtFetch("GET", "/v1/rooms?include_ended=false", state.tokA);
        const rooms = data.rooms || data.items || (Array.isArray(data) ? data : []);
        if (!rooms.some((r) => r.id === state.roomId)) throw new Error(`room ${state.roomId} not in list`);
        return `list size=${rooms.length}`;
      },
    },
    {
      id: "join_a",
      name: "用户 A 入席",
      api: "POST /v1/rooms/:id/join",
      run: async () => {
        require(state.roomId, "房间未创建");
        const data = await Fireside.jwtFetch("POST",
          `/v1/rooms/${encodeURIComponent(state.roomId)}/join`, state.tokA);
        const p = data.participant || data;
        if (!p || p.stage_state !== "on_stage") throw new Error(`stage_state=${p && p.stage_state}`);
        return `stage_state=${p.stage_state}`;
      },
    },
    {
      id: "join_b",
      name: "用户 B 入席",
      api: "POST /v1/rooms/:id/join",
      run: async () => {
        require(state.roomId, "房间未创建");
        const data = await Fireside.jwtFetch("POST",
          `/v1/rooms/${encodeURIComponent(state.roomId)}/join`, state.tokB);
        const p = data.participant || data;
        if (!p || p.stage_state !== "on_stage") throw new Error(`stage_state=${p && p.stage_state}`);
        return `stage_state=${p.stage_state}`;
      },
    },
    {
      id: "get_room",
      name: "房间详情 + 在场列表",
      api: "GET /v1/rooms/:id",
      run: async () => {
        require(state.roomId, "房间未创建");
        const data = await Fireside.jwtFetch("GET",
          `/v1/rooms/${encodeURIComponent(state.roomId)}`, state.tokA);
        const room = data.room || data;
        const parts = data.participants || [];
        if (room.status !== "active") throw new Error(`status=${room.status}`);
        if (parts.length < 2) throw new Error(`participants=${parts.length}, want >=2`);
        return `participants=${parts.length}`;
      },
    },
    {
      id: "ws_a",
      name: "WS 连接 A (hello→welcome)",
      api: "WS /ws/v1/connect",
      run: async () => {
        const r = await openWS(state.tokA);
        state.wsA = r.ws;
        state.nextA = r.next;
        return `user_id=${r.user_id}`;
      },
    },
    {
      id: "ws_b",
      name: "WS 连接 B (hello→welcome)",
      api: "WS /ws/v1/connect",
      run: async () => {
        const r = await openWS(state.tokB);
        state.wsB = r.ws;
        state.nextB = r.next;
        return `user_id=${r.user_id}`;
      },
    },
    {
      id: "sub_a",
      name: "订阅房间 A",
      api: "WS room.subscribe",
      run: async () => {
        require(state.roomId, "房间未创建");
        wsSend(state.wsA, { type: "room.subscribe", room_id: state.roomId });
        const f = await state.nextA("room.subscribed",
          { match: (x) => x.room_id === state.roomId });
        return `conn_id=${(f.conn_id || "").slice(0, 8)}…`;
      },
    },
    {
      id: "sub_b",
      name: "订阅房间 B",
      api: "WS room.subscribe",
      run: async () => {
        require(state.roomId, "房间未创建");
        wsSend(state.wsB, { type: "room.subscribe", room_id: state.roomId });
        const f = await state.nextB("room.subscribed",
          { match: (x) => x.room_id === state.roomId });
        return `conn_id=${(f.conn_id || "").slice(0, 8)}…`;
      },
    },
    {
      id: "msg_send_bcast",
      name: "发送消息 → B 收到广播",
      api: "WS msg.send",
      run: async () => {
        require(state.roomId, "房间未创建");
        const content = `selftest-${Date.now()}`;
        state.msgContent = content;
        const bGot = state.nextB("msg.created",
          { match: (x) => (x.message || {}).content === content });
        wsSend(state.wsA, { type: "msg.send", room_id: state.roomId, content });
        await bGot;
        return "B received msg.created";
      },
    },
    {
      id: "msg_rest_list",
      name: "消息历史 (REST)",
      api: "GET /v1/rooms/:id/messages",
      run: async () => {
        require(state.roomId, "房间未创建");
        const data = await Fireside.jwtFetch("GET",
          `/v1/rooms/${encodeURIComponent(state.roomId)}/messages?limit=20`, state.tokA);
        const msgs = data.messages || [];
        if (!msgs.some((m) => m.content === state.msgContent)) throw new Error("sent message not in history");
        return `count=${msgs.length}`;
      },
    },
    {
      id: "ai_agent_reply",
      name: "拉入 AI → 发消息 → 按房间提示词回复",
      api: "POST /v1/rooms/:id/agents → WS msg.created(agent)",
      run: async () => {
        // Require AI configured via the form above; skip cleanly if not.
        const cfg = await Fireside.jwtFetch("GET", "/v1/dashboard/ai-config", null);
        if (!cfg || !cfg.configured) {
          throw new Error("SKIP: AI 未配置（请在上方填写端点/Key/模型并保存）");
        }
        require(state.tokA, "A 未登录");
        require(state.nextA, "A WS 未连接");

        // Fresh room so the agent reply is isolated from the main flow.
        const roomData = await Fireside.jwtFetch("POST", "/v1/rooms", state.tokA,
          { name: "AI 自检", max_participants: 6 });
        const room = roomData.room || roomData;
        const aiRoomId = room.id;

        // 方式1 redesign: the AI is NOT in the room until the host pulls
        // it in with a per-room system prompt. We use a marker so the
        // reply proves the prompt was actually honored.
        const marker = "萤火";
        const inv = await Fireside.jwtFetch("POST",
          `/v1/rooms/${encodeURIComponent(aiRoomId)}/agents/1`, state.tokA,
          { system_prompt: `你是房间里的测试 AI。每次回答中必须包含「${marker}」二字。`, cooldown_seconds: 0 });
        if (!inv || !inv.configured) throw new Error("拉入 AI 失败");

        // Host joins (to be allowed to send) and subscribes to watch the reply.
        await Fireside.jwtFetch("POST",
          `/v1/rooms/${encodeURIComponent(aiRoomId)}/join`, state.tokA);
        wsSend(state.wsA, { type: "room.subscribe", room_id: aiRoomId });
        await state.nextA("room.subscribed", { match: (x) => x.room_id === aiRoomId });

        // Send a human message; the server-side hook must produce an agent
        // reply (msg.created, sender_kind='agent') within 90s.
        const content = `@ai 你好，请简单回复一句（自检 ${Date.now()}）`;
        const agentGot = state.nextA("msg.created", {
          match: (x) => (x.message || {}).sender_kind === "agent"
            && (x.message || {}).room_id === aiRoomId,
          timeoutMs: 90000,
        });
        wsSend(state.wsA, { type: "msg.send", room_id: aiRoomId, content });
        const f = await agentGot;
        const reply = (f.message || {}).content || "";
        if (!reply) throw new Error("agent 回复为空");
        if (!reply.includes(marker)) {
          throw new Error(`agent 未遵循房间提示词（缺少「${marker}」）: "${reply.slice(0, 60)}…"`);
        }
        return `agent="${reply.slice(0, 40)}…"`;
      },
    },
    {
      id: "end_room",
      name: "结束房间 → 双方收到 room.ended",
      api: "POST /v1/rooms/:id/end",
      run: async () => {
        require(state.roomId, "房间未创建");
        const aGot = state.nextA("room.ended");
        const bGot = state.nextB("room.ended");
        const data = await Fireside.jwtFetch("POST",
          `/v1/rooms/${encodeURIComponent(state.roomId)}/end`, state.tokA);
        if (data.status !== "ended") throw new Error(`status=${data.status}`);
        await aGot;
        await bGot;
        return "both received room.ended";
      },
    },
  ];

  els.runBtn.addEventListener("click", runAll);
  els.aiSaveBtn.addEventListener("click", aiSave);
  els.aiPingBtn.addEventListener("click", aiPing);

  Fireside.ready(async () => {
    renderChecks();
    await aiLoadFromServer();
  });
})();
