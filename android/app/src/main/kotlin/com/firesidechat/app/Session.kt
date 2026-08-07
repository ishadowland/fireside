package com.firesidechat.app

/**
 * In-memory session state shared across activities.
 *
 * baseUrl defaults to the Android emulator's host loopback
 * (10.0.2.2 → the dev machine's localhost). Override with a real
 * host/IP for physical devices.
 */
object Session {
    var baseUrl: String = "http://10.0.2.2:18080"
    val wsUrl: String
        get() = baseUrl.replaceFirst("http://", "ws://").replaceFirst("https://", "wss://")

    var token: String? = null
    var phone: String? = null
    var userId: String? = null
}
