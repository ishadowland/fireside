/* Fireside Dashboard — shared library.
 *
 * Exports a global `Fireside` object with:
 *   - log(level, msg, el): append a log line to el (pre/code block)
 *   - escapeHtml(s): safe HTML-escape user-rendered strings
 *   - jwtLogin(phone, code): returns {token, user_id, expires_in}
 *   - jwtFetch(method, path, token, body?): REST helper
 *   - openWS(token, onFrame): opens /ws/v1/connect, runs auth.hello,
 *     returns {ws, connId}. Fires onFrame(frame, ws) for every incoming
 *     frame including auth.welcome and auth.error.
 *
 * All dashboard pages (rooms.html, room.html) use this module via
 *   <script src="/dashboard/static/lib.js"></script>
 *   <script>Fireside.ready(() => { ... });</script>
 *
 * ADR-0019: served loopback-only by the dashboard router.
 */
(() => {
  "use strict";

  const LIB = {};

  // ---- log + escape -----------------------------------------------------
  LIB.log = function (level, msg, el) {
    if (!el) return;
    const t = new Date().toISOString().split("T")[1].replace("Z", "");
    const line = document.createElement("div");
    line.innerHTML = `<span class="ts">${t}</span> [${level}] ${LIB.escapeHtml(msg)}`;
    line.classList.add(level);
    el.appendChild(line);
    el.scrollTop = el.scrollHeight;
  };

  LIB.escapeHtml = function (s) {
    return String(s).replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[ch]));
  };

  // ---- REST helpers ------------------------------------------------------
  LIB.jwtFetch = async function (method, path, token, body) {
    const init = {
      method,
      headers: {
        "Content-Type": "application/json",
        Authorization: token ? `Bearer ${token}` : "",
      },
    };
    if (body !== undefined) init.body = JSON.stringify(body);
    const res = await fetch(path, init);
    const text = await res.text();
    let json = null;
    if (text) {
      try { json = JSON.parse(text); } catch { /* leave as null */ }
    }
    if (!res.ok) {
      const err = new Error(`HTTP ${res.status}: ${(json && json.error) || text || "unknown"}`);
      err.status = res.status;
      err.body = json;
      throw err;
    }
    return json;
  };

  // ---- Login -------------------------------------------------------------
  // Performs the stub-login flow against /v1/auth/login. Returns
  // {token, expires_in}. Tries GET /v1/dashboard/config first to read
  // the SMS_STUB_CODE; falls back to "1234".
  LIB.login = async function (phone) {
    const cfg = await LIB.jwtFetch("GET", "/v1/dashboard/config", null);
    const code = (cfg && cfg.stub_code) || "1234";
    const body = await LIB.jwtFetch("POST", "/v1/auth/login", null, { phone, code });
    return { token: body.token, expires_in: body.expires_in };
  };

  // ---- WebSocket ---------------------------------------------------------
  // Opens /ws/v1/connect, sends auth.hello synchronously on open, and
  // routes every inbound frame to onFrame(frame, ws). Returns a
  // promise that resolves on welcome (with {ws, connId}) or rejects on
  // auth error or socket close before welcome.
  LIB.openWS = function (token, onFrame) {
    return new Promise((resolve, reject) => {
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const url = `${proto}://${location.host}/ws/v1/connect`;
      const ws = new WebSocket(url);
      let welcomed = false;

      ws.onopen = () => {
        ws.send(JSON.stringify({ type: "auth.hello", token }));
      };

      ws.onmessage = (ev) => {
        let frame;
        try { frame = JSON.parse(ev.data); }
        catch { if (onFrame) onFrame({ type: "_raw", data: ev.data }, ws); return; }

        if (!welcomed && frame.type === "auth.welcome") {
          welcomed = true;
          resolve({ ws, user_id: frame.user_id, jti: frame.jti });
        }
        if (onFrame) onFrame(frame, ws);
      };

      ws.onerror = () => {
        if (!welcomed) reject(new Error("websocket error before welcome"));
      };

      ws.onclose = (ev) => {
        if (!welcomed) {
          reject(new Error(`websocket closed before welcome: ${ev.code} ${ev.reason}`));
        }
      };
    });
  };

  // ---- Time formatting --------------------------------------------------
  // Accepts either a protobuf-style {seconds: N} timestamp or an
  // RFC3339 string (what the REST + WS JSON payloads actually send).
  LIB.fmtTime = function (t) {
    if (t == null) return "";
    if (typeof t === "object" && t.seconds) {
      return new Date(t.seconds * 1000).toLocaleTimeString();
    }
    if (typeof t === "string") {
      const d = new Date(t);
      if (!isNaN(d.getTime())) return d.toLocaleTimeString();
    }
    return "";
  };

  // ---- DOM ready helper --------------------------------------------------
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => {
      // Each page's IIFE asks for this; we dispatch a custom event so
      // script order doesn't matter.
      document.dispatchEvent(new CustomEvent("fireside:ready"));
    });
  } else {
    // DOM already parsed
    queueMicrotask(() => document.dispatchEvent(new CustomEvent("fireside:ready")));
  }
  LIB.ready = function (fn) {
    if (document.readyState === "loading") {
      document.addEventListener("fireside:ready", fn, { once: true });
    } else {
      fn();
    }
  };

  window.Fireside = LIB;
})();
