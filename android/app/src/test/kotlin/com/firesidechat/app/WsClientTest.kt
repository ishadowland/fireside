package com.firesidechat.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pure JVM tests for WsClient's frame parsing branch. Does not require
 * an emulator; org.json is added as testImplementation in build.gradle.kts.
 *
 * WsClient.connect() itself requires OkHttp's WebSocketListener which
 * only runs on Android — those integration tests are out of scope for
 * Sprint 0 (matches the SUB-ANDROID spec §"What you do NOT do").
 */
class WsClientTest {

    @Test
    fun `welcome frame parses to Welcome event`() {
        val frame = JSONObject()
            .put("type", "auth.welcome")
            .put("user_id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("jti", "test-jti-1234")
            .put("server_time", 1722000000L)
            .toString()

        val client = WsClient("ws://localhost:18080")
        // Use reflection to invoke parseFrame — it is private by design.
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected Welcome, got $event", event is WsEvent.Welcome)
        val welcome = event as WsEvent.Welcome
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", welcome.userId)
        assertEquals("test-jti-1234", welcome.jti)
    }

    @Test
    fun `error frame parses to Error event`() {
        val frame = JSONObject()
            .put("type", "auth.error")
            .put("code", "invalid_token")
            .put("error", "JWT expired")
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected Error, got $event", event is WsEvent.Error)
        val err = event as WsEvent.Error
        assertEquals("invalid_token", err.code)
        assertEquals("JWT expired", err.message)
    }

    @Test
    fun `unknown frame type yields bad_frame error`() {
        val frame = JSONObject().put("type", "system.ping").toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected Error, got $event", event is WsEvent.Error)
        val err = event as WsEvent.Error
        assertEquals("bad_frame", err.code)
    }

    @Test
    fun `malformed JSON yields bad_frame error`() {
        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, "not json") as WsEvent

        assertTrue(event is WsEvent.Error)
        assertEquals("bad_frame", (event as WsEvent.Error).code)
    }

    // --- WP-9 business frames ---

    @Test
    fun `room subscribed frame parses to RoomSubscribed`() {
        val frame = JSONObject()
            .put("type", "room.subscribed")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("conn_id", "conn-1")
            .put("server_time", 1722000000L)
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected RoomSubscribed, got $event", event is WsEvent.RoomSubscribed)
        val sub = event as WsEvent.RoomSubscribed
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", sub.roomId)
        assertEquals("conn-1", sub.connId)
        assertEquals(1722000000L, sub.serverTime)
    }

    @Test
    fun `room unsubscribed frame parses to RoomUnsubscribed`() {
        val frame = JSONObject()
            .put("type", "room.unsubscribed")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCD")
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected RoomUnsubscribed, got $event", event is WsEvent.RoomUnsubscribed)
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", (event as WsEvent.RoomUnsubscribed).roomId)
    }

    @Test
    fun `msg created frame parses to MsgCreated with message`() {
        val message = JSONObject()
            .put("id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCE")
            .put("sender_kind", "human")
            .put("sender_id", "01HXYZTESTSAMPLE12345678ABCF")
            .put("content_type", "text")
            .put("content", "hello from test")
            .put("created_at", "2026-08-07T12:34:56.123456+08:00")
        val frame = JSONObject()
            .put("type", "msg.created")
            .put("message", message)
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected MsgCreated, got $event", event is WsEvent.MsgCreated)
        val created = event as WsEvent.MsgCreated
        assertEquals("hello from test", created.message.content)
        assertEquals("human", created.message.senderKind)
        assertEquals("2026-08-07 12:34:56", created.message.shortCreatedAt)
    }

    @Test
    fun `room ended frame parses to RoomEnded`() {
        val frame = JSONObject()
            .put("type", "room.ended")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("ended_by", "01HXYZTESTSAMPLE12345678ABCF")
            .put("server_time", 1722000000L)
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected RoomEnded, got $event", event is WsEvent.RoomEnded)
        val ended = event as WsEvent.RoomEnded
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", ended.roomId)
        assertEquals("01HXYZTESTSAMPLE12345678ABCF", ended.endedBy)
    }

    @Test
    fun `business error frame is non-fatal`() {
        val frame = JSONObject()
            .put("type", "error")
            .put("code", "not_on_stage")
            .put("message", "join first")
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue(event is WsEvent.Error)
        val err = event as WsEvent.Error
        assertEquals("not_on_stage", err.code)
        assertEquals("join first", err.message)
        assertEquals(false, err.fatal)
    }

    @Test
    fun `auth error frame is fatal`() {
        val frame = JSONObject()
            .put("type", "auth.error")
            .put("code", "invalid_token")
            .put("error", "JWT expired")
            .toString()

        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        val err = event as WsEvent.Error
        assertEquals(true, err.fatal)
    }

    @Test
    fun `bad frame is fatal`() {
        val client = WsClient("ws://localhost:18080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, "not json") as WsEvent

        val err = event as WsEvent.Error
        assertEquals(true, err.fatal)
    }
}