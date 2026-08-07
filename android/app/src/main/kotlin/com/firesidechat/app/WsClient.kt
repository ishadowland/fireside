package com.firesidechat.app

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject

/**
 * Thin OkHttp WebSocket wrapper for Fireside (auth handshake + WP-9
 * business frames).
 *
 * Lifecycle:
 *   connect(baseUrl, onEvent)  → emits Connecting, then Open on success
 *   sendHello(token)           → auth.hello between Open and first frame
 *   sendSubscribe(id)          → room.subscribe
 *   sendUnsubscribe(id)        → room.unsubscribe
 *   sendMessage(id, content)   → msg.send
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
                if (evt is WsEvent.Error && evt.fatal) {
                    // Protocol-level failure (auth.error / bad_frame) —
                    // close so onClosed fires and the UI shows the terminal
                    // state. Recoverable business errors do NOT close.
                    webSocket.close(1008, "fatal protocol error")
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
    fun sendHello(token: String): Boolean = sendFrame {
        put("type", "auth.hello")
        put("token", token)
    }

    /** Send room.subscribe. Returns false if the socket is not open. */
    fun sendSubscribe(roomId: String): Boolean = sendFrame {
        put("type", "room.subscribe")
        put("room_id", roomId)
    }

    /** Send room.unsubscribe. Returns false if the socket is not open. */
    fun sendUnsubscribe(roomId: String): Boolean = sendFrame {
        put("type", "room.unsubscribe")
        put("room_id", roomId)
    }

    /** Send msg.send. Returns false if the socket is not open. */
    fun sendMessage(roomId: String, content: String): Boolean = sendFrame {
        put("type", "msg.send")
        put("room_id", roomId)
        put("content", content)
    }

    fun close() {
        socket?.close(1000, "client closed")
        socket = null
    }

    private inline fun sendFrame(build: JSONObject.() -> Unit): Boolean {
        val ws = socket ?: return false
        val frame = JSONObject().apply(build).toString()
        return ws.send(frame)
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
                    fatal = true,
                )
                "room.subscribed" -> WsEvent.RoomSubscribed(
                    roomId = obj.optString("room_id"),
                    connId = obj.optString("conn_id"),
                    serverTime = obj.optLong("server_time"),
                )
                "room.unsubscribed" -> WsEvent.RoomUnsubscribed(
                    roomId = obj.optString("room_id"),
                )
                "msg.created" -> WsEvent.MsgCreated(
                    message = Message.fromJson(obj.getJSONObject("message")),
                )
                "room.ended" -> WsEvent.RoomEnded(
                    roomId = obj.optString("room_id"),
                    endedBy = obj.optString("ended_by"),
                    serverTime = obj.optLong("server_time"),
                )
                "error" -> WsEvent.Error(
                    code = obj.optString("code"),
                    message = obj.optString("message").ifEmpty { obj.optString("error") },
                    fatal = false,
                )
                else -> WsEvent.Error(
                    code = "bad_frame",
                    message = "unexpected type: $type",
                    fatal = true,
                )
            }
        } catch (t: Throwable) {
            WsEvent.Error("bad_frame", "failed to parse: ${t.message}", fatal = true)
        }
    }
}
