package com.firesidechat.app

import android.os.Handler
import android.os.Looper
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Fireside REST client (WP-9). Thin OkHttp wrapper; every callback is
 * marshalled onto the main thread so Activities can safely touch
 * Compose state. Error callbacks receive a short message (server
 * `error` field, or `http_<code>`).
 */
class Api(private val baseUrl: String) {

    private val client = OkHttpClient.Builder()
        .connectTimeout(5, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .build()

    private val main = Handler(Looper.getMainLooper())
    private val jsonMedia = "application/json; charset=utf-8".toMediaType()

    private fun request(method: String, path: String, token: String?, body: JSONObject?): Request {
        val b = Request.Builder().url(baseUrl.trimEnd('/') + path)
        b.method(method, body?.toString()?.toRequestBody(jsonMedia))
        if (token != null) b.header("Authorization", "Bearer $token")
        return b.build()
    }

    private fun enqueue(req: Request, onOk: (JSONObject) -> Unit, onErr: (String) -> Unit) {
        client.newCall(req).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                main.post { onErr(e.message ?: "network_error") }
            }

            override fun onResponse(call: Call, response: Response) {
                val text = response.body?.string() ?: ""
                val code = response.code
                val successful = response.isSuccessful
                response.close()
                main.post {
                    if (successful) {
                        try {
                            onOk(JSONObject(text))
                        } catch (t: Throwable) {
                            onErr("bad_json: ${t.message}")
                        }
                    } else {
                        val msg = try {
                            JSONObject(text).optString("error").ifEmpty { "http_$code" }
                        } catch (_: Throwable) {
                            "http_$code"
                        }
                        onErr(msg)
                    }
                }
            }
        })
    }

    /** GET /v1/dashboard/config → stub SMS code. */
    fun dashboardConfig(onOk: (String) -> Unit, onErr: (String) -> Unit) {
        enqueue(request("GET", "/v1/dashboard/config", null, null), { onOk(it.optString("stub_code")) }, onErr)
    }

    /** POST /v1/auth/login {phone, code} → Token. */
    fun login(phone: String, code: String, onOk: (Token) -> Unit, onErr: (String) -> Unit) {
        val body = JSONObject().put("phone", phone).put("code", code)
        enqueue(request("POST", "/v1/auth/login", null, body), { o ->
            onOk(Token(o.optString("token"), o.optString("refresh_token"), o.optLong("expires_in")))
        }, onErr)
    }

    /** GET /v1/rooms → active rooms. */
    fun listRooms(token: String, onOk: (List<Room>) -> Unit, onErr: (String) -> Unit) {
        enqueue(request("GET", "/v1/rooms", token, null), { o ->
            onOk(parseArray(o.optJSONArray("rooms")) { Room.fromJson(it) })
        }, onErr)
    }

    /** POST /v1/rooms {name, max_participants} → created room. */
    fun createRoom(token: String, name: String, maxParticipants: Int = 6, onOk: (Room) -> Unit, onErr: (String) -> Unit) {
        val body = JSONObject().put("name", name).put("max_participants", maxParticipants)
        enqueue(request("POST", "/v1/rooms", token, body), { o ->
            onOk(Room.fromJson(o.getJSONObject("room")))
        }, onErr)
    }

    /** POST /v1/rooms/:id/join. */
    fun joinRoom(token: String, roomId: String, onOk: () -> Unit, onErr: (String) -> Unit) {
        enqueue(request("POST", "/v1/rooms/$roomId/join", token, null), { onOk() }, onErr)
    }

    /** GET /v1/rooms/:id → room + participants. */
    fun roomDetail(token: String, roomId: String, onOk: (Room, List<Participant>) -> Unit, onErr: (String) -> Unit) {
        enqueue(request("GET", "/v1/rooms/$roomId", token, null), { o ->
            val room = Room.fromJson(o.getJSONObject("room"))
            val participants = parseArray(o.optJSONArray("participants")) { Participant.fromJson(it) }
            onOk(room, participants)
        }, onErr)
    }

    /** GET /v1/rooms/:id/messages → message history (chronological). */
    fun listMessages(token: String, roomId: String, onOk: (List<Message>) -> Unit, onErr: (String) -> Unit) {
        enqueue(request("GET", "/v1/rooms/$roomId/messages?limit=50", token, null), { o ->
            val msgs = parseArray(o.optJSONArray("messages")) { Message.fromJson(it) }
            onOk(msgs.sortedBy { m -> m.createdAt })
        }, onErr)
    }

    private fun <T> parseArray(arr: JSONArray?, from: (org.json.JSONObject) -> T): List<T> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).map { i -> from(arr.getJSONObject(i)) }
    }
}
