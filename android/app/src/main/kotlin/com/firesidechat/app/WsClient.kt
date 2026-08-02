package com.firesidechat.app

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject

/**
 * Thin OkHttp WebSocket wrapper for the Fireside Sprint 0 auth handshake.
 *
 * Lifecycle:
 *   connect(baseUrl, onEvent)  → emits Connecting, then Open on success
 *   sendHello(token)           → must be called between Open and the first
 *                                server frame; returns false if not yet open
 *   close()                    → tears down the connection cleanly
 *
 * The baseUrl is the ws://host:port root. The library appends the
 * canonical "/ws/v1/connect" path per the SUB-003 contract.
 */
class WsClient(
    private val baseUrl: String,
    private val connectPath: String = "/ws/v1/connect",
) {
    private val client = OkHttpClient()
    private var socket: WebSocket? = null
    private var pendingToken: String? = null

    /**
     * Open a WS connection to `${baseUrl}${connectPath}`.
     *
     * @param pendingToken optional JWT to auto-send as the auth.hello frame
     *        once the socket opens. If null, the caller must invoke
     *        [sendHello] explicitly when they have a token.
     * @param onEvent invoked on every connection lifecycle event. The
     *        caller is responsible for marshalling back onto the main
     *        thread (this class does not switch threads).
     */
    fun connect(pendingToken: String? = null, onEvent: (WsEvent) -> Unit) {
        this.pendingToken = pendingToken
        onEvent(WsEvent.Connecting)

        val url = (baseUrl.trimEnd('/') + connectPath).also {
            require(it.startsWith("ws://") || it.startsWith("wss://")) {
                "baseUrl must be ws:// or wss:// (got: $baseUrl)"
            }
        }
        val request = Request.Builder().url(url).build()

        socket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                onEvent(WsEvent.Open)
                pendingToken?.let { sendHello(it) }
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val evt = parseFrame(text)
                onEvent(evt)
                if (evt is WsEvent.Error) {
                    // Server signaled a protocol-level error — close so
                    // onClosed fires and the UI shows the terminal state.
                    webSocket.close(1008, "auth failed")
                }
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                onEvent(WsEvent.Closed)
                webSocket.close(code, reason)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                onEvent(WsEvent.Failure(t))
            }
        })
    }

    /**
     * Send the auth.hello frame. Returns false if the socket is not yet
     * open (caller should retry once [WsEvent.Open] arrives).
     */
    fun sendHello(token: String): Boolean {
        val ws = socket ?: return false
        val frame = JSONObject().apply {
            put("type", "auth.hello")
            put("token", token)
        }.toString()
        return ws.send(frame)
    }

    fun close() {
        socket?.close(1000, "client closed")
        socket = null
    }

    private fun parseFrame(text: String): WsEvent {
        return try {
            val obj = JSONObject(text)
            when (val type = obj.optString("type")) {
                "auth.welcome" -> WsEvent.Welcome(
                    userId = obj.optString("user_id"),
                    jti = obj.optString("jti"),
                )
                "auth.error" -> WsEvent.Error(
                    code = obj.optString("code"),
                    message = obj.optString("error"),
                )
                else -> WsEvent.Error(
                    code = "bad_frame",
                    message = "unexpected type: $type",
                )
            }
        } catch (t: Throwable) {
            WsEvent.Error("bad_frame", "failed to parse: ${t.message}")
        }
    }
}