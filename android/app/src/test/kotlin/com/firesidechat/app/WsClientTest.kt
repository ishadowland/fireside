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
            .put("user_id", 42L)
            .put("jti", "test-jti-1234")
            .put("server_time", 1722000000L)
            .toString()

        val client = WsClient("ws://localhost:8080")
        // Use reflection to invoke parseFrame — it is private by design.
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected Welcome, got $event", event is WsEvent.Welcome)
        val welcome = event as WsEvent.Welcome
        assertEquals(42L, welcome.userId)
        assertEquals("test-jti-1234", welcome.jti)
    }

    @Test
    fun `error frame parses to Error event`() {
        val frame = JSONObject()
            .put("type", "auth.error")
            .put("code", "invalid_token")
            .put("error", "JWT expired")
            .toString()

        val client = WsClient("ws://localhost:8080")
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

        val client = WsClient("ws://localhost:8080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, frame) as WsEvent

        assertTrue("expected Error, got $event", event is WsEvent.Error)
        val err = event as WsEvent.Error
        assertEquals("bad_frame", err.code)
    }

    @Test
    fun `malformed JSON yields bad_frame error`() {
        val client = WsClient("ws://localhost:8080")
        val method = client.javaClass.getDeclaredMethod("parseFrame", String::class.java)
        method.isAccessible = true
        val event = method.invoke(client, "not json") as WsEvent

        assertTrue(event is WsEvent.Error)
        assertEquals("bad_frame", (event as WsEvent.Error).code)
    }
}