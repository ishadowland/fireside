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
  };

  const state = { code: "1234", tokA: null, tokB: null, roomId: null };
  const results = new Map(); // id -> {ok, ms}

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
        pill = r.ok
          ? `<span class="status-pill status-active">PASS</span>`
          : `<span class="status-pill status-ended">FAIL</span>`;
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
    const pass = [...results.values()].filter((r) => r.ok).length;
    els.summaryCount.textContent = `${pass} / ${CHECKS.length}`;
    if (!done) {
      els.summaryStatus.textContent = "运行中…";
      return;
    }
    const fail = CHECKS.length - pass;
    els.summaryStatus.textContent = fail === 0
      ? `全部通过 (${pass}/${CHECKS.length})`
      : `${fail} 项失败 (${pass}/${CHECKS.length})`;
    els.summaryStatus.className = "status " + (fail === 0 ? "ok" : "err");
  }

  async function runCheck(c) {
    const t0 = performance.now();
    let detail = "";
    try {
      detail = (await c.run()) || "";
      results.set(c.id, { ok: true, ms: Math.round(performance.now() - t0) });
      log("ok", `PASS  ${c.api}${detail ? " — " + detail : ""}`);
    } catch (err) {
      results.set(c.id, { ok: false, ms: Math.round(performance.now() - t0) });
      log("err", `FAIL  ${c.api} — ${err.message}`);
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

  Fireside.ready(renderChecks);
})();
