package com.firesidechat.app

/**
 * Connection lifecycle events emitted by [WsClient] to the UI layer.
 *
 * Mirrors the SUB-ANDROID handoff contract
 * (docs/handoff/sprint0/SUB-ANDROID-connect-activity.md §Interface contract).
 */
sealed class WsEvent {
    object Connecting : WsEvent()
    object Open : WsEvent()
    data class Welcome(val userId: Long, val jti: String) : WsEvent()
    data class Error(val code: String, val message: String) : WsEvent()
    object Closed : WsEvent()
    data class Failure(val cause: Throwable) : WsEvent()
}