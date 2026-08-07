package com.firesidechat.app

/**
 * Connection lifecycle events emitted by [WsClient] to the UI layer.
 *
 * Mirrors the SUB-ANDROID handoff contract
 * (docs/handoff/sprint0/SUB-ANDROID-connect-activity.md §Interface contract)
 * plus the WP-9 business frames (Sprint 1.5).
 */
sealed class WsEvent {
    object Connecting : WsEvent()
    object Open : WsEvent()
    // Sprint 1-3 (ADR-0014): user_id on the wire is a 26-char ULID string.
    data class Welcome(val userId: String, val jti: String) : WsEvent()

    /**
     * @param fatal true for protocol-level failures (auth.error, bad_frame)
     *        after which the socket should be closed; false for recoverable
     *        business errors (e.g. room_not_found, not_on_stage).
     */
    data class Error(val code: String, val message: String, val fatal: Boolean = false) : WsEvent()

    object Closed : WsEvent()
    data class Failure(val cause: Throwable) : WsEvent()

    // Sprint 1.5 (WP-9): business frames (internal/ws/business_frames.go).
    data class RoomSubscribed(val roomId: String, val connId: String, val serverTime: Long) : WsEvent()
    data class RoomUnsubscribed(val roomId: String) : WsEvent()
    data class MsgCreated(val message: Message) : WsEvent()
    data class RoomEnded(val roomId: String, val endedBy: String, val serverTime: Long) : WsEvent()
}
