/* Fireside Dashboard — Agent 管理器 (issue #38).
 *
 * Persisted agent presets: 接入方式 / host:端口 / api token / 模型 / 提示词.
 * All calls go to the loopback-only /v1/dashboard/agents API. The api token
 * is write-only — GET never returns it (only has_token), so editing keeps
 * the stored token unless a new one is typed.
 */
(() => {
  "use strict";

  const KINDS = {
    openai: "OpenAI 兼容",
    simple: "简单 API",
    openclaw: "OpenClaw 兼容",
  };

  const els = {
    tbody: document.getElementById("preset-tbody"),
    msg: document.getElementById("preset-msg"),
    form: document.getElementById("preset-form"),
    title: document.getElementById("form-title"),
    name: document.getElementById("p-name"),
    kind: document.getElementById("p-kind"),
    model: document.getElementById("p-model"),
    host: document.getElementById("p-host"),
    port: document.getElementById("p-port"),
    baseUrl: document.getElementById("p-base-url"),
    token: document.getElementById("p-token"),
    prompt: document.getElementById("p-prompt"),
    saveBtn: document.getElementById("save-btn"),
    cancelBtn: document.getElementById("cancel-btn"),
    formStatus: document.getElementById("form-status"),
  };

  let editingId = null;

  function setMsg(text, ok) {
    els.msg.textContent = text || "";
    els.msg.className = "status" + (ok === true ? " ok" : ok === false ? " err" : "");
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[ch]));
  }

  async function api(method, path, body) {
    const init = { method, headers: { "Content-Type": "application/json" } };
    if (body !== undefined) init.body = JSON.stringify(body);
    const res = await fetch(path, init);
    const text = await res.text();
    let json = null;
    if (text) { try { json = JSON.parse(text); } catch { /* ignore */ } }
    if (!res.ok) {
      throw new Error((json && json.error) || `HTTP ${res.status}`);
    }
    return json;
  }

  async function loadPresets() {
    els.tbody.innerHTML = `<tr><td colspan="6" class="empty">加载中…</td></tr>`;
    try {
      const d = await api("GET", "/v1/dashboard/agents");
      const list = (d && d.agents) || [];
      renderList(list);
    } catch (err) {
      els.tbody.innerHTML = `<tr><td colspan="6" class="empty">加载失败: ${escapeHtml(err.message)}</td></tr>`;
    }
  }

  function renderList(list) {
    if (!list.length) {
      els.tbody.innerHTML = `<tr><td colspan="6" class="empty">暂无预置，先在下方新建一个。</td></tr>`;
      return;
    }
    els.tbody.innerHTML = list.map((p) => `
      <tr>
        <td>${escapeHtml(p.name)}</td>
        <td>${escapeHtml(KINDS[p.kind] || p.kind)}</td>
        <td class="hint">${escapeHtml(p.endpoint || "")}</td>
        <td class="hint">${escapeHtml(p.model || "")}</td>
        <td>${p.has_token ? "已设置" : "—"}</td>
        <td>
          <button data-act="edit" data-id="${escapeHtml(p.id)}">编辑</button>
          <button data-act="ping" data-id="${escapeHtml(p.id)}">测试</button>
          <button data-act="del" data-id="${escapeHtml(p.id)}" class="danger">删除</button>
        </td>
      </tr>`).join("");
  }

  function fillForm(p) {
    els.name.value = p.name || "";
    els.kind.value = p.kind || "openai";
    els.model.value = p.model || "";
    els.host.value = "";
    els.port.value = "443";
    els.baseUrl.value = p.endpoint || "";
    els.prompt.value = p.system_prompt || "";
    els.token.value = "";
    // Try to split the resolved endpoint back into host:port for editing.
    const m = /^https?:\/\/([^/:]+):(\d+)\/?$/.exec(p.endpoint || "");
    if (m) {
      els.host.value = m[1];
      els.port.value = m[2];
      els.baseUrl.value = "";
    } else if (p.endpoint) {
      els.baseUrl.value = p.endpoint;
    }
  }

  function resetForm() {
    editingId = null;
    els.title.textContent = "新建 Agent 预置";
    els.cancelBtn.hidden = true;
    els.form.reset();
    els.port.value = "443";
    els.formStatus.textContent = "";
  }

  function startEdit(id, presets) {
    const p = presets.find((x) => x.id === id);
    if (!p) return;
    editingId = id;
    fillForm(p);
    els.title.textContent = `编辑: ${p.name}`;
    els.cancelBtn.hidden = false;
    els.formStatus.textContent = "留空 Token 则保留原值";
    els.formStatus.className = "status";
    window.scrollTo({ top: document.body.scrollHeight, behavior: "smooth" });
  }

  els.tbody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-act]");
    if (!btn) return;
    const id = btn.dataset.id;
    try {
      if (btn.dataset.act === "ping") {
        btn.disabled = true;
        btn.textContent = "测试中…";
        const r = await api("POST", `/v1/dashboard/agents/${encodeURIComponent(id)}/ping`);
        setMsg(`测试通过 · 延迟 ${r.latency_ms}ms`, true);
      } else if (btn.dataset.act === "del") {
        if (!confirm("删除该 Agent 预置？已拉入的房间不受影响（将退回全局配置）。")) return;
        await api("DELETE", `/v1/dashboard/agents/${encodeURIComponent(id)}`);
        setMsg("已删除。", true);
      } else if (btn.dataset.act === "edit") {
        const d = await api("GET", "/v1/dashboard/agents");
        startEdit(id, (d && d.agents) || []);
        return;
      }
      await loadPresets();
    } catch (err) {
      setMsg(`操作失败: ${err.message}`, false);
    } finally {
      loadPresets();
    }
  });

  els.cancelBtn.addEventListener("click", resetForm);

  els.form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const body = {
      name: els.name.value.trim(),
      kind: els.kind.value,
      host: els.host.value.trim(),
      port: parseInt(els.port.value, 10) || 0,
      base_url: els.baseUrl.value.trim(),
      api_token: els.token.value,
      model: els.model.value.trim(),
      system_prompt: els.prompt.value,
    };
    els.saveBtn.disabled = true;
    try {
      if (editingId) {
        await api("POST", `/v1/dashboard/agents/${encodeURIComponent(editingId)}`, body);
        setMsg("已更新。", true);
      } else {
        await api("POST", "/v1/dashboard/agents", body);
        setMsg("已保存（持久化到本地文件）。", true);
      }
      resetForm();
      await loadPresets();
    } catch (err) {
      els.formStatus.className = "status err";
      els.formStatus.textContent = `保存失败: ${err.message}`;
    } finally {
      els.saveBtn.disabled = false;
    }
  });

  Fireside.ready(loadPresets);
})();
