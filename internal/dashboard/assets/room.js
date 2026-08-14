/* Fireside Dashboard — room.html (chat).
 *
 * UX flow:
 *   1. On load: stub-login → POST /:id/join (tolerates already_on_stage;
 *      the host is not auto-joined on create) → fetch /v1/rooms/:id
 *      (room info + participants) → WS connect + room.subscribe.
 *   2. Render messages received via WS frames (msg.created, system,
 *      room.ended).
 *   3. Send button → POST /v1/rooms/:id/messages and the REST handler
 *      will fan out via the hub (issue #18). The sender ALSO receives
 *      a broadcast (we broadcast to all subscribers) so the message
 *      appears in the chat without an extra round-trip.
 *   4. "End Room" button → POST /v1/rooms/:id/end. The hub broadcasts
 *      a room.ended frame; we close the chat input.
 *
 * ADR-0019 loopback-only. ADR-0021 snake_case JSON.
 */
(() => {
  "use strict";

  const TEST_PHONE = "+8613800138000";

  const els = {
    title: document.getElementById("room-title"),
    status: document.getElementById("room-status"),
    meId: document.getElementById("me-id"),
    participants: document.getElementById("participants-list"),
    messages: document.getElementById("messages"),
    msgInput: document.getElementById("msg-input"),
    sendBtn: document.getElementById("send-btn"),
    endBtn: document.getElementById("end-btn"),
    exportBtn: document.getElementById("export-btn"),
    wsStatus: document.getElementById("ws-status"),
    log: document.getElementById("log"),
  };

  // AI assistant labels. Both agent IDs share the same 8-char prefix
  // ("01AGT000"), so a naive truncation makes them look identical; label
  // by known ID and fall back to the trailing chars.
  const AGENT_LABELS = {
    "01AGT000000000000000000000": "AI 助手 1",
    "01AGT000000000000000000001": "AI 助手 2",
  };
  const AGENT_SLOTS = {
    "01AGT000000000000000000000": 1,
    "01AGT000000000000000000001": 2,
  };
  function senderLabel(id) {
    if (!id) return "?";
    return AGENT_LABELS[id] || `${id.slice(0, 8)}…`;
  }

  // AI assistant slots (1..2): each has its own prompt + cooldown +
  // invite/remove controls, wired by slot below.
  const SLOTS = [1, 2];
  const agentEls = {};
  for (const slot of SLOTS) {
    agentEls[slot] = {
      status: document.getElementById(`agent-status-${slot}`),
      panel: document.getElementById(`agent-panel-${slot}`),
      preset: document.getElementById(`agent-preset-${slot}`),
      cooldown: document.getElementById(`agent-cooldown-${slot}`),
      invite: document.getElementById(`agent-invite-${slot}`),
      remove: document.getElementById(`agent-remove-${slot}`),
      msg: document.getElementById(`agent-msg-${slot}`),
    };
  }

  // Free-speech round controls.
  const fsEls = {
    status: document.getElementById("fs-status"),
    panel: document.getElementById("fs-panel"),
    minutes: document.getElementById("fs-minutes"),
    toggle: document.getElementById("fs-toggle-btn"),
    msg: document.getElementById("fs-msg"),
  };
  let fsEnabled = false;
  let fsTick = null;

  // Per-slot temporary-ban state, kept in sync from GET /agents/:slot.
  const agentMuted = {};

  // Extract room id from URL: /dashboard/rooms/:id
  const ROOM_ID = (() => {
    const m = location.pathname.match(/\/dashboard\/rooms\/([^/?#]+)/);
    return m ? decodeURIComponent(m[1]) : null;
  })();

  let token = null;
  let meId = null;
  let hostUserId = null;
  let ws = null;
  let subscribedRoomId = null;
  let roomEnded = false;

  function setWsStatus(text, ok) {
    els.wsStatus.textContent = text;
    els.wsStatus.className = "status " + (ok === true ? "ok" : ok === false ? "err" : "");
  }

  function setRoomStatus(text, status) {
    els.status.textContent = text;
    if (status) {
      els.status.className = "status-pill status-" + status;
    }
  }

  function appendMessage(msg) {
    const isSystem = msg.sender_kind === "system";
    const row = document.createElement("div");
    row.className = "msg " + (isSystem ? "msg-system" : (msg.sender_id === meId ? "msg-mine" : "msg-other"));
    const sender = isSystem ? "system" : senderLabel(msg.sender_id);
    const time = Fireside.fmtTime(msg.created_at);
    const content = msg.content || "";
    row.innerHTML = `
      <span class="msg-sender">${Fireside.escapeHtml(sender)}</span>
      <span class="msg-time">${time}</span>
      <div class="msg-body">${Fireside.escapeHtml(content)}</div>
    `;
    // Clear empty placeholder if present.
    const empty = els.messages.querySelector(".empty");
    if (empty) empty.remove();
    els.messages.appendChild(row);
    els.messages.scrollTop = els.messages.scrollHeight;
  }

  function renderParticipants(list) {
    if (!list || !list.length) {
      els.participants.innerHTML = `<li class="empty">无人在场</li>`;
      return;
    }
    els.participants.innerHTML = list.map((p) => {
      const me = (p.user_id === meId) ? " (me)" : "";
      const stage = p.stage_state || "?";
      const isAgent = stage === "agent";
      const slot = AGENT_SLOTS[p.user_id];
      const name = isAgent ? senderLabel(p.user_id) : `${(p.user_id || "").slice(0, 8)}…`;
      const stageText = isAgent ? "在场 (AI)" : stage;
      const isHost = hostUserId && hostUserId === meId;
      let ops = "";
      if (isAgent && slot && isHost) {
        const muted = !!(agentMuted[slot] && agentMuted[slot].muted);
        ops = `<span class="agent-ops">
          <button data-action="remove-agent" data-slot="${slot}" title="把该助手移出房间">移除</button>
          <button data-action="toggle-mute" data-slot="${slot}" title="${muted ? "解除禁止" : "暂时禁止该助手发言"}">${muted ? "解禁" : "禁止"}</button>
        </span>`;
      }
      return `<li class="stage-${Fireside.escapeHtml(stage)}">
        <code>${Fireside.escapeHtml(name)}</code>
        <span class="stage">${Fireside.escapeHtml(stageText)}${Fireside.escapeHtml(me)}</span>
        ${ops}
      </li>`;
    }).join("");
  }

  async function ensureOnStage() {
    // msg.send (WS) and POST /messages require the sender to be a
    // participant on_stage. Joining from the lobby already put us on
    // stage; a direct visit (or "create room" navigation, where the
    // host is NOT auto-joined) needs an explicit join here. Tolerate
    // 409 already_on_stage (we're already a participant).
    try {
      await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/join`, token);
    } catch (err) {
      if (!(err.status === 409 && err.body && err.body.error === "already_on_stage")) {
        throw err;
      }
    }
  }

  async function loadRoom() {
    try {
      const data = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}`, token);
      const room = data.room || data;
      els.title.textContent = room.name || "房间";
      hostUserId = room.host_user_id || null;
      setRoomStatus(room.status || "active", room.status);
      Fireside.log("ok", `loaded room ${room.id} status=${room.status}`, els.log);
    } catch (err) {
      Fireside.log("err", `loadRoom failed: ${err.message}`, els.log);
    }
  }

  async function loadMessages() {
    try {
      // The API returns newest-first, paged via ?since=next_before.
      // Fetch every page then reverse so the chat renders oldest-first.
      const msgs = [];
      let since = "";
      for (let i = 0; i < 50; i++) {
        const qs = new URLSearchParams({ limit: "500" });
        if (since) qs.set("since", since);
        const d = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/messages?${qs}`, token);
        const page = (d && d.messages) || [];
        msgs.push(...page);
        since = (d && d.next_before) || "";
        if (!since || page.length === 0) break;
      }
      msgs.reverse();
      for (const m of msgs) appendMessage(m);
      if (msgs.length === 0) {
        els.messages.innerHTML = `<div class="empty">暂无消息</div>`;
      }
      Fireside.log("ok", `loaded ${msgs.length} history messages`, els.log);
    } catch (err) {
      Fireside.log("err", `loadMessages failed: ${err.message}`, els.log);
    }
  }

  async function loadParticipants() {
    try {
      // GET /v1/rooms/:id returns { room, participants } — there is
      // no separate /participants REST endpoint in Sprint 1.
      const data = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}`, token);
      const list = data.participants || data.items || (Array.isArray(data) ? data : []);
      renderParticipants(list);
    } catch (err) {
      Fireside.log("err", `loadParticipants failed: ${err.message}`, els.log);
    }
  }

  async function postMessage() {
    const content = (els.msgInput.value || "").trim();
    if (!content || roomEnded) return;
    try {
      await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/messages`, token, { content });
      els.msgInput.value = "";
    } catch (err) {
      Fireside.log("err", `send failed: ${err.message}`, els.log);
    }
  }

  async function endRoom() {
    if (!confirm("确定结束房间？所有在场者将被断开。")) return;
    els.endBtn.disabled = true;
    try {
      await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/end`, token);
      Fireside.log("ok", "room end requested", els.log);
    } catch (err) {
      Fireside.log("err", `endRoom failed: ${err.message}`, els.log);
      els.endBtn.disabled = false;
    }
  }

  // ---- export conversation as Markdown ----------------------------------
  // Fetches every message in the room (paged via ?since=next_before) and
  // downloads a .md transcript of the current conversation window.
  async function exportMarkdown() {
    els.exportBtn.disabled = true;
    els.exportBtn.textContent = "导出中…";
    try {
      const msgs = [];
      let since = "";
      for (let i = 0; i < 50; i++) {
        const qs = new URLSearchParams({ limit: "500" });
        if (since) qs.set("since", since);
        const d = await Fireside.jwtFetch(
          "GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/messages?${qs}`, token);
        const page = (d && d.messages) || [];
        msgs.push(...page);
        since = (d && d.next_before) || "";
        if (!since || page.length === 0) break;
      }
      const md = buildMarkdown(els.title.textContent || "房间", msgs);
      const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `conversation-${ROOM_ID}.md`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      Fireside.log("ok", `exported ${msgs.length} messages`, els.log);
    } catch (err) {
      Fireside.log("err", `export failed: ${err.message}`, els.log);
    } finally {
      els.exportBtn.disabled = false;
      els.exportBtn.textContent = "导出 MD";
    }
  }

  function buildMarkdown(roomName, msgs) {
    const lines = [];
    lines.push(`# ${roomName}`, "");
    lines.push(`> 房间: ${ROOM_ID} · 导出时间: ${new Date().toLocaleString()} · 共 ${msgs.length} 条消息`);
    lines.push("", "---", "");
    for (const m of msgs) {
      const who = m.sender_kind === "system"
        ? "系统"
        : m.sender_kind === "agent"
          ? senderLabel(m.sender_id)
          : m.sender_id === meId ? "我" : senderLabel(m.sender_id);
      const at = Fireside.fmtTime(m.created_at) || "";
      lines.push(`### ${who}${at ? " · " + at : ""}`, "");
      const body = String(m.content || "");
      body.split("\n").forEach((l) => lines.push(`> ${l}`));
      lines.push("");
    }
    return lines.join("\n");
  }

  async function loadAgentState(slot) {
    const el = agentEls[slot];
    try {
      const data = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/${slot}`, token);
      agentMuted[slot] = {
        muted: !!(data && data.muted),
        remaining: (data && data.muted_remaining_seconds) || 0,
      };
      renderAgentState(slot, data);
      await loadParticipants();
    } catch (err) {
      el.status.textContent = `获取失败: ${err.message}`;
      el.status.className = "status err";
    }
  }

  function renderAgentState(slot, state) {
    const el = agentEls[slot];
    const isHost = hostUserId && hostUserId === meId;
    const configured = !!(state && state.configured);
    const muted = !!(state && state.muted);
    const remaining = (state && state.muted_remaining_seconds) || 0;
    el.status.textContent = configured
      ? `已拉入 · ${senderLabel(state.agent_id)}${state.preset_name ? ` · ${state.preset_name}` : ""}${muted ? ` · 已禁止(${fmtDur(remaining)})` : ""}`
      : "未拉入";
    el.status.className = "status " + (configured ? (muted ? "muted" : "ok") : "");
    el.panel.hidden = !isHost;
    if (!isHost) return;
    el.invite.textContent = configured ? "更新预置" : "拉入房间";
    el.remove.hidden = !configured;
    if (configured) {
      if (state.preset_id && el.preset.value !== state.preset_id) {
        el.preset.value = state.preset_id;
      }
      el.cooldown.value = String(state.cooldown_seconds ?? 0);
    }
  }

  // Loads the saved Agent presets (loopback /v1/dashboard/agents — the
  // dashboard page is loopback-only, so this never leaks to other users).
  // Each slot's select is populated; the invite payload only carries the
  // preset id, never the endpoint/token.
  async function loadPresets() {
    try {
      const d = await Fireside.jwtFetch("GET", "/v1/dashboard/agents", null);
      const list = (d && d.agents) || [];
      for (const slot of SLOTS) {
        const el = agentEls[slot];
        const prev = el.preset.value;
        el.preset.innerHTML = `<option value="">选择 Agent 预置…</option>` +
          list.map((p) => `<option value="${Fireside.escapeHtml(p.id)}">${Fireside.escapeHtml(p.name)} (${Fireside.escapeHtml(p.kind)})</option>`).join("");
        el.preset.value = prev;
      }
    } catch (err) {
      Fireside.log("err", `loadPresets failed: ${err.message}`, els.log);
    }
  }

  async function inviteAgent(slot) {
    const el = agentEls[slot];
    const presetId = el.preset.value;
    const raw = parseInt(el.cooldown.value, 10);
    const cooldown = isNaN(raw) || raw < 0 ? 0 : Math.min(raw, 3600);
    el.invite.disabled = true;
    el.msg.className = "status";
    el.msg.textContent = "拉入中…";
    try {
      await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/${slot}`, token,
        { agent_preset_id: presetId, cooldown_seconds: cooldown });
      Fireside.log("ok", `slot ${slot}: invited agent (preset=${presetId || "<global>"}, cooldown=${cooldown}s)`, els.log);
      el.msg.className = "status ok";
      el.msg.textContent = "已拉入。发一条消息试试,AI 会自动回复。";
      await loadAgentState(slot);
    } catch (err) {
      el.msg.className = "status err";
      el.msg.textContent = `拉入失败: ${err.message}`;
      Fireside.log("err", `inviteAgent(${slot}) failed: ${err.message}`, els.log);
    } finally {
      el.invite.disabled = false;
    }
  }

  async function removeAgent(slot) {
    const el = agentEls[slot];
    if (!confirm(`移除 AI 助手 ${slot}？`)) return;
    el.remove.disabled = true;
    el.msg.className = "status";
    el.msg.textContent = "移除中…";
    try {
      await Fireside.jwtFetch("DELETE", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/${slot}`, token);
      Fireside.log("ok", `slot ${slot}: agent removed`, els.log);
      el.msg.className = "status ok";
      el.msg.textContent = "已移除。";
      el.cooldown.value = "0";
      await loadAgentState(slot);
    } catch (err) {
      el.msg.className = "status err";
      el.msg.textContent = `移除失败: ${err.message}`;
      Fireside.log("err", `removeAgent(${slot}) failed: ${err.message}`, els.log);
    } finally {
      el.remove.disabled = false;
    }
  }

  // ---- host controls on the 在场 list (remove / temporarily ban) -------
  // Formats a seconds count as mm:ss (or "∞" when empty).
  function fmtDur(sec) {
    if (!sec || sec < 0) return "∞";
    const mm = Math.floor(sec / 60);
    const ss = sec % 60;
    return `${mm}:${String(ss).padStart(2, "0")}`;
  }

  // Delegated click handler for the per-agent buttons rendered in
  // renderParticipants (only shown to the host).
  async function onParticipantAction(e) {
    const btn = e.target.closest("button[data-action]");
    if (!btn) return;
    const slot = parseInt(btn.dataset.slot, 10);
    if (isNaN(slot)) return;
    if (btn.dataset.action === "remove-agent") await removeAgentFromStage(slot);
    else if (btn.dataset.action === "toggle-mute") await toggleAgentMute(slot);
  }

  async function removeAgentFromStage(slot) {
    if (!confirm(`将 AI 助手 ${slot} 移出房间？`)) return;
    try {
      await Fireside.jwtFetch("DELETE", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/${slot}`, token);
      Fireside.log("ok", `slot ${slot}: agent removed from stage`, els.log);
      agentMuted[slot] = { muted: false, remaining: 0 };
      await loadAgentState(slot);
      await loadParticipants();
    } catch (err) {
      Fireside.log("err", `removeAgentFromStage(${slot}) failed: ${err.message}`, els.log);
    }
  }

  async function toggleAgentMute(slot) {
    const cur = agentMuted[slot] || {};
    const enabling = !cur.muted;
    let minutes = 30;
    if (enabling) {
      const v = prompt(`禁止 AI 助手 ${slot} 发言多久？(分钟, 1-240)`);
      if (v === null) return;
      const n = parseInt(v, 10);
      minutes = (isNaN(n) || n < 1) ? 30 : Math.min(n, 240);
    }
    try {
      const d = await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/${slot}/mute`, token,
        { enabled: enabling, minutes: enabling ? minutes : 0 });
      agentMuted[slot] = {
        muted: !!(d && d.muted),
        remaining: (d && d.muted_remaining_seconds) || 0,
      };
      Fireside.log("ok", `slot ${slot}: ${enabling ? "banned" : "unbanned"}`, els.log);
      await loadAgentState(slot);
      await loadParticipants();
    } catch (err) {
      Fireside.log("err", `toggleAgentMute(${slot}) failed: ${err.message}`, els.log);
    }
  }

  function onFrame(frame) {
    Fireside.log("in", `< ${frame.type}${frame.code ? " " + frame.code : ""}`, els.log);    switch (frame.type) {
      case "msg.created":
        if (frame.message) appendMessage(frame.message);
        break;
      case "room.ended":
        roomEnded = true;
        setRoomStatus("ended", "ended");
        els.msgInput.disabled = true;
        els.sendBtn.disabled = true;
        els.endBtn.disabled = true;
        els.exportBtn.disabled = true;
        if (fsTick) clearInterval(fsTick);
        // Refresh participants to show empty.
        loadParticipants();
        break;
      case "participant.joined":
      case "participant.left":
        loadParticipants();
        break;
      case "error":
        Fireside.log("err", `WS error: ${frame.code} ${frame.message || ""}`, els.log);
        break;
    }
  }

  async function connectAndSubscribe() {
    try {
      const r = await Fireside.openWS(token, onFrame);
      ws = r.ws;
      setWsStatus("open", true);
      // Subscribe to this room.
      ws.send(JSON.stringify({
        type: "room.subscribe",
        room_id: ROOM_ID,
      }));
      subscribedRoomId = ROOM_ID;
      Fireside.log("ok", `subscribed to ${ROOM_ID} (jti=${r.jti})`, els.log);
    } catch (err) {
      setWsStatus(`failed: ${err.message}`, false);
      Fireside.log("err", `WS failed: ${err.message}`, els.log);
    }
  }

  // ---- free-speech round ---------------------------------------------
  async function loadFreeSpeech() {
    try {
      const d = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/free-speech`, token);
      renderFreeSpeech(d);
    } catch (err) {
      fsEls.status.textContent = `获取失败: ${err.message}`;
      fsEls.status.className = "status err";
    }
  }

  function renderFreeSpeech(d) {
    const isHost = hostUserId && hostUserId === meId;
    fsEnabled = !!(d && d.enabled);
    if (fsEnabled) {
      const rem = (d && d.remaining_seconds != null) ? d.remaining_seconds : 0;
      const mm = Math.floor(rem / 60);
      const ss = rem % 60;
      fsEls.status.textContent = `进行中 · 剩余 ${mm}:${String(ss).padStart(2, "0")}`;
      fsEls.status.className = "status ok";
    } else {
      fsEls.status.textContent = "未开启（AI 只在收到人类消息时回复）";
      fsEls.status.className = "status";
    }
    fsEls.panel.hidden = !isHost;
    fsEls.toggle.textContent = fsEnabled ? "停止" : "开启";
    fsEls.toggle.disabled = false;

    // Live countdown while a round is running + auto-refresh on expiry.
    clearInterval(fsTick);
    fsTick = null;
    if (fsEnabled) {
      fsTick = setInterval(loadFreeSpeech, 1000);
    }
  }

  async function toggleFreeSpeech() {
    const start = !fsEnabled;
    const mins = parseInt(fsEls.minutes.value, 10);
    const round = (isNaN(mins) || mins < 1) ? 5 : Math.min(mins, 60);
    fsEls.toggle.disabled = true;
    fsEls.msg.className = "status";
    fsEls.msg.textContent = start ? "开启中…" : "停止中…";
    try {
      const d = await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(ROOM_ID)}/agents/free-speech`, token,
        { enabled: start, round_seconds: start ? round * 60 : 0 });
      renderFreeSpeech(d);
      Fireside.log("ok", `free speech ${start ? `started (${round} min)` : "stopped"}`, els.log);
      fsEls.msg.textContent = start ? "已开启，AI 助手开始自由发言。" : "已停止。";
    } catch (err) {
      fsEls.msg.className = "status err";
      fsEls.msg.textContent = `${start ? "开启" : "停止"}失败: ${err.message}`;
      Fireside.log("err", `toggleFreeSpeech failed: ${err.message}`, els.log);
    } finally {
      fsEls.toggle.disabled = false;
    }
  }

  // ---- wire up DOM -------------------------------------------------------
  els.sendBtn.addEventListener("click", postMessage);
  els.msgInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") postMessage();
  });
  els.endBtn.addEventListener("click", endRoom);
  els.exportBtn.addEventListener("click", exportMarkdown);
  els.participants.addEventListener("click", onParticipantAction);
  for (const slot of SLOTS) {
    agentEls[slot].invite.addEventListener("click", () => inviteAgent(slot));
    agentEls[slot].remove.addEventListener("click", () => removeAgent(slot));
  }
  fsEls.toggle.addEventListener("click", toggleFreeSpeech);

  Fireside.ready(async () => {
    try {
      const r = await Fireside.login(TEST_PHONE);
      token = r.token;
      // Decode user_id (display only).
      try {
        const payload = JSON.parse(atob(token.split(".")[1]));
        meId = payload.user_id || payload.sub || null;
      } catch { /* ignore */ }
      els.meId.textContent = meId || "(?)";
      await ensureOnStage();
      await loadRoom();
      await loadParticipants();
      await connectAndSubscribe();
      await loadMessages();
      await loadPresets();
      for (const slot of SLOTS) await loadAgentState(slot);
      await loadFreeSpeech();
      // After WS open + subscribe, enable chat input.
      if (ws) {
        els.msgInput.disabled = false;
        els.sendBtn.disabled = false;
        els.endBtn.disabled = false;
        els.exportBtn.disabled = false;
      }
    } catch (err) {
      Fireside.log("err", `init failed: ${err.message}`, els.log);
    }
  });
})();
