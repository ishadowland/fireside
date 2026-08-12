/* Fireside Dashboard — admin.html (管理后台).
 *
 * Drive the loopback-only admin API (/v1/admin, ADR-0019):
 *   - list every room (active + ended) with participant/message counts
 *   - force-close any room regardless of host
 *   - delete a room's records (participants + messages cascade)
 *   - clear ALL rooms
 *
 * No JWT — the loopback gate is the auth. ADR-0021 snake_case JSON.
 */
(() => {
  "use strict";

  const els = {
    refreshBtn: document.getElementById("refresh-btn"),
    clearAllBtn: document.getElementById("clear-all-btn"),
    listStatus: document.getElementById("list-status"),
    tbody: document.getElementById("admin-tbody"),
    log: document.getElementById("log"),
  };

  function setStatus(msg, ok) {
    els.listStatus.textContent = msg;
    els.listStatus.className = "status " + (ok === true ? "ok" : ok === false ? "err" : "");
  }

  function appendRow(r) {
    const status = r.status || "active";
    const t = (fmt) => (fmt ? Fireside.fmtTime(r[fmt]) : "");
    return `<tr data-id="${Fireside.escapeHtml(r.id)}">
      <td>${Fireside.escapeHtml(r.name || "")}</td>
      <td><span class="status-pill status-${Fireside.escapeHtml(status)}">${Fireside.escapeHtml(status)}</span></td>
      <td><code>${Fireside.escapeHtml((r.host_user_id || "").slice(0, 8))}…</code></td>
      <td>${r.max_participants ?? ""}</td>
      <td>${r.participant_count ?? ""}</td>
      <td>${r.message_count ?? ""}</td>
      <td>${t("created_at")}</td>
      <td class="row-actions">
        <button class="close-btn" ${status !== "active" ? "disabled" : ""}>强制关闭</button>
        <button class="delete-btn danger">删除记录</button>
      </td>
    </tr>`;
  }

  async function listRooms() {
    setStatus("加载中…", null);
    try {
      const data = await Fireside.jwtFetch("GET", "/v1/admin/rooms", null);
      const rooms = data.rooms || [];
      els.tbody.innerHTML = rooms.length
        ? rooms.map(appendRow).join("")
        : `<tr><td colspan="8" class="empty">暂无房间记录</td></tr>`;
      bindRowButtons();
      setStatus(`共 ${rooms.length} 个房间`, true);
    } catch (err) {
      setStatus(err.message, false);
      Fireside.log("err", `listRooms failed: ${err.message}`, els.log);
    }
  }

  function bindRowButtons() {
    els.tbody.querySelectorAll(".close-btn").forEach((btn) => {
      btn.addEventListener("click", () => doClose(btn));
    });
    els.tbody.querySelectorAll(".delete-btn").forEach((btn) => {
      btn.addEventListener("click", () => doDelete(btn));
    });
  }

  function rowId(btn) {
    return btn.closest("tr").dataset.id;
  }

  async function doClose(btn) {
    const id = rowId(btn);
    if (!confirm(`强制关闭房间 ${id} ?`)) return;
    btn.disabled = true;
    try {
      const data = await Fireside.jwtFetch("POST", `/v1/admin/rooms/${encodeURIComponent(id)}/close`, null);
      Fireside.log("ok", `closed ${data.room_id} (${data.status})`, els.log);
    } catch (err) {
      Fireside.log("err", `close ${id} failed: ${err.message}`, els.log);
    }
    btn.disabled = false;
    await listRooms();
  }

  async function doDelete(btn) {
    const id = rowId(btn);
    if (!confirm(`删除房间 ${id} 的全部记录(参与者 + 消息)?`)) return;
    btn.disabled = true;
    try {
      const data = await Fireside.jwtFetch("DELETE", `/v1/admin/rooms/${encodeURIComponent(id)}`, null);
      Fireside.log("ok", `deleted room ${data.room_id} (records removed)`, els.log);
    } catch (err) {
      Fireside.log("err", `delete ${id} failed: ${err.message}`, els.log);
    }
    btn.disabled = false;
    await listRooms();
  }

  async function clearAll() {
    if (!confirm("确定清空全部房间的记录(所有参与者 + 消息)?\n此操作不可恢复。")) return;
    els.clearAllBtn.disabled = true;
    try {
      const data = await Fireside.jwtFetch("DELETE", "/v1/admin/rooms", null);
      Fireside.log("ok", `cleared all rooms (${data.deleted} deleted)`, els.log);
      await listRooms();
    } catch (err) {
      Fireside.log("err", `clearAll failed: ${err.message}`, els.log);
    } finally {
      els.clearAllBtn.disabled = false;
    }
  }

  els.refreshBtn.addEventListener("click", listRooms);
  els.clearAllBtn.addEventListener("click", clearAll);

  listRooms();
})();