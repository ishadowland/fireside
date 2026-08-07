package com.firesidechat.app

import org.json.JSONObject

/**
 * Wire models for the Fireside REST + WS business frames (WP-9).
 *
 * Field names mirror the server's Go structs (internal types.go files).
 */
data class Token(
    val token: String,
    val refreshToken: String,
    val expiresIn: Long,
)

data class Room(
    val id: String,
    val hostUserId: String,
    val name: String,
    val maxParticipants: Int,
    val status: String,
    val announcement: String?,
    val createdAt: String,
    val endedAt: String?,
) {
    val shortCreatedAt: String
        get() = createdAt.take(19).replace('T', ' ')

    companion object {
        fun fromJson(o: JSONObject): Room = Room(
            id = o.optString("id"),
            hostUserId = o.optString("host_user_id"),
            name = o.optString("name"),
            maxParticipants = o.optInt("max_participants"),
            status = o.optString("status"),
            announcement = if (o.isNull("announcement")) null else o.optString("announcement"),
            createdAt = o.optString("created_at"),
            endedAt = if (o.isNull("ended_at")) null else o.optString("ended_at"),
        )
    }
}

data class Participant(
    val id: String,
    val userId: String,
    val stageState: String,
) {
    companion object {
        fun fromJson(o: JSONObject): Participant = Participant(
            id = o.optString("id"),
            userId = o.optString("user_id"),
            stageState = o.optString("stage_state"),
        )
    }
}

/**
 * A message. Shared by REST GET /v1/rooms/:id/messages and the WS
 * msg.created broadcast (whose `message` field is a MessageView).
 */
data class Message(
    val id: String,
    val roomId: String,
    val senderKind: String,
    val senderId: String?,
    val contentType: String,
    val content: String,
    val createdAt: String,
) {
    /** RFC3339 like `2026-08-07T12:34:56.789123+08:00` → `2026-08-07 12:34:56`. */
    val shortCreatedAt: String
        get() = createdAt.take(19).replace('T', ' ')

    companion object {
        fun fromJson(o: JSONObject): Message = Message(
            id = o.optString("id"),
            roomId = o.optString("room_id"),
            senderKind = o.optString("sender_kind"),
            senderId = if (o.isNull("sender_id")) null else o.optString("sender_id"),
            contentType = o.optString("content_type"),
            content = o.optString("content"),
            createdAt = o.optString("created_at"),
        )
    }
}
