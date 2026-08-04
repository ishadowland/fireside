/* Fireside Dashboard — rooms.html (lobby).
 *
 * UX flow:
 *   1. On load: stub-login (idempotent with the index page — same
 *      TEST_PHONE gets the same user_id).
 *   2. Fetch /v1/rooms (active list) and render the table.
 *   3. "Create" button → POST /v1/rooms, then re-list.
 *   4. "Join" button → POST /v1/rooms/:id/join, then navigate to
 *      /dashboard/rooms/:id (chat).
 *
 * ADR-0019: loopback-only. ADR-0021: snake_case JSON.
 */
(() => {
  "use strict";

  const TEST_PHONE = "+861****8000";

  const els = {
    meId: document.getElementById("me-id"),
    newName: document.getElementById("new-name"),
    newMax: document.getElementById("new-max"),
    createBtn: document.getElementById("create-btn"),
    createStatus: document.getElementById("create-status"),
    refreshBtn: document.getElementById("refresh-btn"),
    refreshStatus: document.getElementById("refresh-status"),
    tbody: document.getElementById("rooms-tbody"),
    log: document.getElementById("log"),
  };

  let token = null;
  let meId = null;

  function setStatus(el, msg, ok) {
    el.textContent = msg;
    el.className = "status " + (ok === true ? "ok" : ok === false ? "err" : "");
  }

  async function listRooms() {
    setStatus(els.refreshStatus, "加载中…", null);
    try {
      const data = await Fireside.jwtFetch("GET", "/v1/rooms?include_ended=false", token);
      const rooms = data.rooms || data.items || (Array.isArray(data) ? data : []);
      render(rooms);
      setStatus(els.refreshStatus, `共 ${rooms.length} 个`, true);
    } catch (err) {
      setStatus(els.refreshStatus, err.message, false);
      Fireside.log("err", `listRooms failed: ${err.message}`, els.log);
    }
  }

  function render(rooms) {
    if (!rooms.length) {
      els.tbody.innerHTML = `<tr><td colspan="6" class="empty">暂无活跃房间</td></tr>`;
      return;
    }
    els.tbody.innerHTML = rooms.map(r => {
      const status = r.status || "active";
      const created = Fireside.fmtTime(r.created_at);
      return `<tr data-id="${Fireside.escapeHtml(r.id)}">
        <td>${Fireside.escapeHtml(r.name || "")}</td>
        <td><span class="status-pill status-${Fireside.escapeHtml(status)}">${Fireside.escapeHtml(status)}</span></td>
        <td><code>${Fireside.escapeHtml((r.host_user_id || "").slice(0, 8))}…</code></td>
        <td>${r.max_participants ?? ""}</td>
        <td>${created}</td>
        <td><button class="join-btn">加入</button></td>
      </tr>`;
    }).join("");

    // Bind buttons
    els.tbody.querySelectorAll(".join-btn").forEach((btn) => {
      btn.addEventListener("click", async () => {
        const row = btn.closest("tr");
        const id = row.dataset.id;
        await joinRoom(id);
      });
    });
  }

  async function createRoom() {
    const name = (els.newName.value || "").trim();
    const max = parseInt(els.newMax.value, 10) || 8;
    if (!name) {
      setStatus(els.createStatus, "请输入房间名", false);
      return;
    }
    els.createBtn.disabled = true;
    setStatus(els.createStatus, "创建中…", null);
    try {
      const data = await Fireside.jwtFetch("POST", "/v1/rooms", token, {
        name, max_participants: max,
      });
      const room = data.room || data;
      Fireside.log("ok", `created room ${room.id} (${room.name})`, els.log);
      setStatus(els.createStatus, `已创建 ${room.id} — 正在打开…`, true);
      // Briefly settle, then navigate to chat.
      setTimeout(() => {
        location.href = `/dashboard/rooms/${encodeURIComponent(room.id)}`;
      }, 400);
    } catch (err) {
      setStatus(els.createStatus, err.message, false);
      Fireside.log("err", `createRoom failed: ${err.message}`, els.log);
    } finally {
      els.createBtn.disabled = false;
    }
  }

  async function joinRoom(roomId) {
    Fireside.log("info", `POST /v1/rooms/${roomId}/join`, els.log);
    try {
      await Fireside.jwtFetch("POST", `/v1/rooms/${encodeURIComponent(roomId)}/join`, token);
      Fireside.log("ok", `joined room ${roomId}`, els.log);
      location.href = `/dashboard/rooms/${encodeURIComponent(roomId)}`;
    } catch (err) {
      Fireside.log("err", `join failed: ${err.message}`, els.log);
    }
  }

  els.createBtn.addEventListener("click", createRoom);
  els.refreshBtn.addEventListener("click", listRooms);
  els.newName.addEventListener("keydown", (e) => {
    if (e.key === "Enter") createRoom();
  });

  Fireside.ready(async () => {
    try {
      const r = await Fireside.login(TEST_PHONE);
      token = r.token;
      meId = "(see localStorage)";
      // Decode JWT payload for user_id (no verify — display only).
      try {
        const payload = JSON.parse(atob(token.split(".")[1]));
        meId = payload.user_id || payload.sub || "?";
      } catch { /* ignore */ }
      els.meId.textContent = meId;
      await listRooms();
    } catch (err) {
      Fireside.log("err", `login failed: ${err.message}`, els.log);
    }
  });
})();
