/* Fireside Dashboard — mirrors the Android ConnectActivity flow:
   auto stub-login -> WS connect -> auth.hello -> welcome. ADR-0019. */
(() => {
  "use strict";

  const els = {
    loginStatus: document.getElementById("login-status"),
    loginPhone: document.getElementById("login-phone"),
    loginToken: document.getElementById("login-token"),
    wsUrl: document.getElementById("ws-url"),
    wsStatus: document.getElementById("ws-status"),
    connectBtn: document.getElementById("connect-btn"),
    disconnectBtn: document.getElementById("disconnect-btn"),
    log: document.getElementById("log"),
  };

  const TEST_PHONE = "+8613800138000";
  let ws = null;
  let token = null;

  function log(level, msg) {
    const t = new Date().toISOString().split("T")[1].replace("Z", "");
    const line = document.createElement("div");
    line.innerHTML = `<span class="ts">${t}</span> [${level}] ${escapeHtml(msg)}`;
    line.classList.add(level);
    els.log.appendChild(line);
    els.log.scrollTop = els.log.scrollHeight;
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[ch]));
  }

  function setLoginStatus(text, ok) {
    els.loginStatus.textContent = text;
    els.loginStatus.className = ok === undefined ? "" : ok ? "ok" : "err";
  }

  function setWsStatus(text, cls) {
    els.wsStatus.textContent = text;
    els.wsStatus.className = cls || "";
  }

  function setButtons(connected) {
    els.connectBtn.disabled = connected || !token;
    els.disconnectBtn.disabled = !connected;
  }

  async function autoLogin() {
    try {
      log("info", "fetching /v1/dashboard/config for stub code");
      const cfg = await (await fetch("/v1/dashboard/config")).json();
      const code = cfg.stub_code || "1234";
      els.loginPhone.textContent = TEST_PHONE;

      log("info", `POST /v1/auth/login {phone: ${TEST_PHONE}, code: ${"*".repeat(code.length)}}`);
      const res = await fetch("/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ phone: TEST_PHONE, code }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(`login ${res.status}: ${body.error || "unknown"}`);

      token = body.token;
      els.loginToken.textContent = token;
      setLoginStatus(`ok — expires_in=${body.expires_in}s`, true);
      log("ok", "login succeeded, JWT received");
      setButtons(false);
    } catch (err) {
      setLoginStatus(`failed: ${err.message}`, false);
      log("err", `auto-login failed: ${err.message}`);
    }
  }

  function wsURL() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    return `${proto}://${location.host}/ws/v1/connect`;
  }

  function connect() {
    els.wsUrl.textContent = wsURL();
    log("info", `connecting ${wsURL()}`);
    setWsStatus("connecting…");

    ws = new WebSocket(wsURL());
    let helloSent = false;

    ws.onopen = () => {
      setWsStatus("open, awaiting welcome…", "ok");
      log("out", "> {type: auth.hello}");
      ws.send(JSON.stringify({ type: "auth.hello", token }));
      helloSent = true;
    };

    ws.onmessage = (ev) => {
      let frame;
      try {
        frame = JSON.parse(ev.data);
      } catch {
        log("err", `bad frame: ${ev.data}`);
        return;
      }
      log("in", `< ${ev.data}`);
      switch (frame.type) {
        case "auth.welcome":
          setWsStatus(`welcome — user_id=${frame.user_id} jti=${frame.jti}`, "ok");
          break;
        case "auth.error":
          setWsStatus(`auth error: ${frame.code} ${frame.error}`, "err");
          break;
        default:
          log("info", `unexpected type: ${frame.type}`);
      }
    };

    ws.onerror = () => {
      setWsStatus("error", "err");
      log("err", "websocket error");
    };

    ws.onclose = (ev) => {
      setWsStatus(`closed (${ev.code})`, ev.code === 1000 ? "" : "err");
      log("info", `websocket closed code=${ev.code} reason=${ev.reason}`);
      ws = null;
      setButtons(false);
    };

    setButtons(true);
  }

  function disconnect() {
    if (ws) ws.close(1000, "dashboard closed");
  }

  els.connectBtn.addEventListener("click", connect);
  els.disconnectBtn.addEventListener("click", disconnect);
  els.wsUrl.textContent = wsURL();

  autoLogin();
})();
