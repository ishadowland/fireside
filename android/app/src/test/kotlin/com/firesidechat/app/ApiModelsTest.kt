package com.firesidechat.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Pure JVM tests for the wire-model parsing used by Api (org.json only,
 * no Android/Looper — Api itself needs Robolectric, so we test the
 * parse helpers it delegates to).
 */
class ApiModelsTest {

    private fun roomJson(
        id: String = "01HXYZTESTSAMPLE12345678ABCD",
        ended: Boolean = false,
    ): JSONObject = JSONObject()
        .put("id", id)
        .put("host_user_id", "01HXYZTESTSAMPLE12345678ABCF")
        .put("name", "test-room")
        .put("max_participants", 6)
        .put("status", if (ended) "ended" else "active")
        .put("announcement", JSONObject.NULL)
        .put("created_at", "2026-08-07T12:34:56.123456+08:00")
        .apply { if (ended) put("ended_at", "2026-08-07T13:00:00+08:00") }

    @Test
    fun `room parses all fields`() {
        val room = Room.fromJson(roomJson())
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", room.id)
        assertEquals("01HXYZTESTSAMPLE12345678ABCF", room.hostUserId)
        assertEquals("test-room", room.name)
        assertEquals(6, room.maxParticipants)
        assertEquals("active", room.status)
        assertNull(room.announcement)
        assertEquals("2026-08-07 12:34:56", room.shortCreatedAt)
        assertNull(room.endedAt)
    }

    @Test
    fun `room with ended_at parses ended status`() {
        val room = Room.fromJson(roomJson(ended = true))
        assertEquals("ended", room.status)
        assertEquals("2026-08-07T13:00:00+08:00", room.endedAt)
    }

    @Test
    fun `message parses with nullable sender_id`() {
        val json = JSONObject()
            .put("id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCE")
            .put("sender_kind", "system")
            .put("sender_id", JSONObject.NULL)
            .put("content_type", "text")
            .put("content", "room started")
            .put("created_at", "2026-08-07T12:00:00.000000Z")

        val msg = Message.fromJson(json)
        assertEquals("system", msg.senderKind)
        assertNull(msg.senderId)
        assertEquals("room started", msg.content)
        assertEquals("2026-08-07 12:00:00", msg.shortCreatedAt)
    }

    @Test
    fun `participant parses`() {
        val json = JSONObject()
            .put("id", "01HXYZTESTSAMPLE12345678ABCD")
            .put("room_id", "01HXYZTESTSAMPLE12345678ABCE")
            .put("user_id", "01HXYZTESTSAMPLE12345678ABCF")
            .put("stage_state", "on_stage")

        val p = Participant.fromJson(json)
        assertEquals("01HXYZTESTSAMPLE12345678ABCD", p.id)
        assertEquals("01HXYZTESTSAMPLE12345678ABCF", p.userId)
        assertEquals("on_stage", p.stageState)
    }
}
