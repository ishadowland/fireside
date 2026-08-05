/* Fireside Dashboard — room.html (chat).
 *
 * UX flow:
 *   1. On load: stub-login → fetch /v1/rooms/:id (room info) → fetch
 *      /v1/rooms/:id/participants (presence list) → WS connect + room.subscribe.
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

  const TEST_PHONE = "+861****8000";

  const els = {
    title: document.getElementById("room-title"),
    status: document.getElementById("room-status"),
    meId: document.getElementById("me-id"),
    participants: document.getElementById("participants-list"),
    messages: document.getElementById("messages"),
    msgInput: document.getElementById("msg-input"),
    sendBtn: document.getElementById("send-btn"),
    endBtn: document.getElementById("end-btn"),
    wsStatus: document.getElementById("ws-status"),
    log: document.getElementById("log"),
  };

  // Extract room id from URL: /dashboard/rooms/:id
  const ROOM_ID = (() => {
    const m = location.pathname.match(/\/dashboard\/rooms\/([^/?#]+)/);
    return m ? decodeURIComponent(m[1]) : null;
  })();

  let token = null;
  let meId = null;
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
    const sender = isSystem ? "system" : (msg.sender_id ? msg.sender_id.slice(0, 8) + "…" : "?");
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
      return `<li class="stage-${Fireside.escapeHtml(stage)}">
        <code>${Fireside.escapeHtml((p.user_id || "").slice(0, 8))}…</code>
        <span class="stage">${Fireside.escapeHtml(stage)}${Fireside.escapeHtml(me)}</span>
      </li>`;
    }).join("");
  }

  async function loadRoom() {
    try {
      const data = await Fireside.jwtFetch("GET", `/v1/rooms/${encodeURIComponent(ROOM_ID)}`, token);
      const room = data.room || data;
      els.title.textContent = room.name || "房间";
      setRoomStatus(room.status || "active", room.status);
      Fireside.log("ok", `loaded room ${room.id} status=${room.status}`, els.log);
    } catch (err) {
      Fireside.log("err", `loadRoom failed: ${err.message}`, els.log);
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

  function onFrame(frame) {
    Fireside.log("in", `< ${frame.type}${frame.code ? " " + frame.code : ""}`, els.log);
    switch (frame.type) {
      case "msg.created":
        if (frame.message) appendMessage(frame.message);
        break;
      case "room.ended":
        roomEnded = true;
        setRoomStatus("ended", "ended");
        els.msgInput.disabled = true;
        els.sendBtn.disabled = true;
        els.endBtn.disabled = true;
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

  // ---- wire up DOM -------------------------------------------------------
  els.sendBtn.addEventListener("click", postMessage);
  els.msgInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") postMessage();
  });
  els.endBtn.addEventListener("click", endRoom);

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
      await loadRoom();
      await loadParticipants();
      await connectAndSubscribe();
      // After WS open + subscribe, enable chat input.
      if (ws) {
        els.msgInput.disabled = false;
        els.sendBtn.disabled = false;
        els.endBtn.disabled = false;
      }
    } catch (err) {
      Fireside.log("err", `init failed: ${err.message}`, els.log);
    }
  });
})();
