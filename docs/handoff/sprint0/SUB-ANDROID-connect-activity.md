# SUB-ANDROID: Android Compose client — `ConnectActivity`

- **Subcontract ID**: SUB-ANDROID
- **Parent RFC**: `docs/rfc/phase-1-mvp.md` §Sprint 0 task breakdown (Afternoon + Evening slots, Android half)
- **Out scope for**: An Android developer (Kotlin + Jetpack Compose + OkHttp WebSocket). No Go, no backend knowledge required.
- **Depends on** (must already exist in `main`):
  - Nothing Go-specific. SUB-ANDROID is **fully independent** of the Go server.
  - The only contract is: server exposes `ws://<host>:18080/ws/v1/connect` and expects the first frame to be `{"type":"auth.hello","token":"<jwt>"}`. Replies with `auth.welcome` or `auth.error`.
- **NOT in scope**: any UI beyond a single status screen. Login flow, room list, message UI — all Sprint 1+.

## What you deliver

A complete Android project at `android/` that builds with `./gradlew assembleDebug` and runs on an API 24+ emulator. The app shows:

- On launch: connect to backend, show "⏳ connecting…"
- On WS open: send `auth.hello` with a token the user pastes into a text field
- On `auth.welcome` received: show "✅ connected" + the `user_id` + `jti`
- On `auth.error` or close: show "❌ <error_code>"

No state management library beyond what Compose provides natively. No Hilt/Koin. No Room database — that is Sprint 1+ (ADR-0005).

## Architecture (file layout — exactly this)

```
android/
├── settings.gradle.kts
├── build.gradle.kts                 # root
├── gradle.properties
├── gradle/wrapper/
│   ├── gradle-wrapper.jar
│   └── gradle-wrapper.properties    # gradle 8.7+
├── gradlew
├── gradlew.bat
└── app/
    ├── build.gradle.kts
    ├── proguard-rules.pro
    └── src/
        ├── main/
        │   ├── AndroidManifest.xml
        │   ├── kotlin/com/firesidechat/app/
        │   │   ├── MainActivity.kt
        │   │   ├── ConnectActivity.kt     # the only screen in Sprint 0
        │   │   ├── WsClient.kt            # OkHttp WebSocket wrapper
        │   │   └── ui/theme/Theme.kt      # Material 3 theme
        │   └── res/
        │       ├── values/
        │       │   ├── strings.xml
        │       │   ├── colors.xml
        │       │   └── themes.xml
        │       └── mipmap-*/ic_launcher.* # any default launcher icon
        └── test/
            └── kotlin/com/firesidechat/app/
                └── WsClientTest.kt        # optional but encouraged
```

Package name: `com.firesidechat.app` (matches the Android decision in `docs/requirements/03-decision-snapshot.md`).

## Interface contract (locked)

```kotlin
// WsClient.kt
package com.firesidechat.app

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject

sealed class WsEvent {
    object Connecting : WsEvent()
    object Open : WsEvent()
    data class Welcome(val userId: Long, val jti: String) : WsEvent()
    data class Error(val code: String, val message: String) : WsEvent()
    object Closed : WsEvent()
    data class Failure(val cause: Throwable) : WsEvent()
}

class WsClient(private val baseUrl: String) {  // e.g. "ws://10.0.2.2:18080"
    private val client = OkHttpClient()
    private var socket: WebSocket? = null

    fun connect(onEvent: (WsEvent) -> Unit) { ... }
    fun sendHello(token: String): Boolean { ... }  // returns false if socket not open
    fun close() { ... }
}
```

The URL format: `ws://<host>:<port>/ws/v1/connect`. For emulator → host, host is `10.0.2.2`.

```kotlin
// ConnectActivity.kt
//
// UI: a single screen with:
//   - TextField: WebSocket base URL (default "ws://10.0.2.2:18080/ws/v1/connect")
//   - TextField: JWT token (user pastes from `curl` output)
//   - Button: "Connect & Hello"
//   - Status text: shows current WsEvent
//   - On Welcome: also shows user_id and jti
```

## Implementation plan (steps you follow)

### Step 1 — Project scaffold (~30 min)

Two options:

**Option A** (recommended for an experienced Android dev): hand-write `settings.gradle.kts` + root `build.gradle.kts` + `app/build.gradle.kts` + wrapper. Use Gradle 8.7, AGP 8.5+, Kotlin 2.0+, Compose BOM 2024.09+.

**Option B**: `gradle init` in a temp directory, copy the wrapper files into `android/`, then write the rest manually. Faster if you don't remember AGP version syntax.

Key `app/build.gradle.kts` settings:
- `compileSdk = 34`
- `minSdk = 24` (Android 7.0) — decision in `docs/reviews/pdcp-checklist.md` §deferred
- `targetSdk = 34`
- Kotlin JVM target 17
- Compose enabled
- Dependencies: `androidx.activity:activity-compose`, `androidx.compose.material3:material3`, `com.squareup.okhttp3:okhttp:4.12.0`

### Step 2 — `WsClient.kt` (~30 min)

Wrap `OkHttpClient.newWebSocket(request, listener)`:

- `connect(onEvent)`: build `Request` from `baseUrl + "/ws/v1/connect"`, call `client.newWebSocket(...)`, store the handle.
- `WebSocketListener` overrides:
  - `onOpen`: emit `WsEvent.Open`, then call `sendHello(pendingToken)` if one was queued.
  - `onMessage(text)`: parse JSON, branch on `type`:
    - `"auth.welcome"` → emit `Welcome(userId, jti)`
    - `"auth.error"` → emit `Error(code, error)`
    - else → emit `Error("bad_frame", text)` (defensive)
  - `onClosing(code, reason)`: emit `Closed`
  - `onFailure(t)`: emit `Failure(t)`

Use `org.json.JSONObject` (bundled with Android, no extra dep needed).

### Step 3 — `ConnectActivity.kt` (~30 min)

```kotlin
class ConnectActivity : ComponentActivity() {
    override fun onCreate(...) {
        setContent {
            FiresideTheme {
                Surface { ConnectScreen() }
            }
        }
    }
}

@Composable
fun ConnectScreen() {
    var url by remember { mutableStateOf("ws://10.0.2.2:18080/ws/v1/connect") }
    var token by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<WsEvent>(WsEvent.Closed) }
    val client = remember { WsClient("ws://10.0.2.2:18080") }  // base host:port only

    Column(...) {
        OutlinedTextField(value = url, onValueChange = { url = it }, label = { Text("WS URL") })
        OutlinedTextField(value = token, onValueChange = { token = it }, label = { Text("JWT token") })
        Button(onClick = { client.connect { e -> status = e }; /* sendHello queued or triggered on open */ }) {
            Text("Connect & Hello")
        }
        Text(text = when (status) {
            WsEvent.Closed -> "Status: closed"
            WsEvent.Connecting -> "Status: connecting…"
            WsEvent.Open -> "Status: open, awaiting welcome…"
            is WsEvent.Welcome -> "✅ connected — user_id=${status.userId} jti=${status.jti}"
            is WsEvent.Error -> "❌ ${status.code}: ${status.message}"
            is WsEvent.Failure -> "❌ failure: ${status.cause.message}"
        })
    }
}
```

To send hello, expose a second button or auto-send on `Open`. Recommend auto-send on `Open`: in `WsClient`, accept `pendingToken` via constructor or setter, then `sendHello(pendingToken)` inside `onOpen`.

### Step 4 — `MainActivity.kt` (~5 min)

Just `setContent { FiresideTheme { ConnectScreen() } }`. Or skip `MainActivity` and make `ConnectActivity` the launcher.

### Step 5 — Manifest + theme (~15 min)

`AndroidManifest.xml`:
```xml
<manifest>
  <uses-permission android:name="android.permission.INTERNET" />
  <application
      android:label="@string/app_name"
      android:theme="@style/Theme.Fireside"
      android:usesCleartextTraffic="true">  <!-- Sprint 0 only; remove for HTTPS -->
    <activity android:name=".ConnectActivity" android:exported="true">
      <intent-filter>
        <action android:name="android.intent.action.MAIN" />
        <category android:name="android.intent.category.LAUNCHER" />
      </intent-filter>
    </activity>
  </application>
</manifest>
```

`usesCleartextTraffic="true"` is required because Sprint 0 backend is plain HTTP/WS (RFC §"What we are NOT doing"). **Add a TODO comment** to remove this before any HTTPS deployment.

`res/values/themes.xml`: inherit from `Theme.Material3.DayNight.NoActionBar`.

### Step 6 — Build + emulator smoke (~20 min)

```
cd android
./gradlew assembleDebug
# launch emulator (any API 24+ system image)
./gradlew installDebug
adb shell am start -n com.firesidechat.app/.ConnectActivity
```

Manual acceptance on emulator:

1. App opens, shows "Status: closed".
2. Paste a token from the curl output (start backend first, get token).
3. Tap "Connect & Hello".
4. Status transitions: connecting → open → "✅ connected".
5. Kill backend: status flips to "❌ failure: …".

## Acceptance criteria (binary pass/fail)

- [ ] `cd android && ./gradlew assembleDebug` exits 0
- [ ] `cd android && ./gradlew installDebug` succeeds on an API 24+ emulator
- [ ] Launching the app shows the Connect screen with default URL `ws://10.0.2.2:18080/ws/v1/connect`
- [ ] Smoke test against running backend:
  1. Get token via curl from SUB-001
  2. Paste into app, tap Connect
  3. UI shows "✅ connected — user_id=<N> jti=<uuid>"
- [ ] No files modified outside `android/`
- [ ] `usesCleartextTraffic="true"` has a `// TODO: remove before HTTPS deployment` comment
- [ ] APK is debug-signed (release signing is Sprint 1+)

## What you do NOT do

- ❌ Do not add any HTTP/REST client — only WebSocket in Sprint 0
- ❌ Do not add Room/database
- ❌ Do not add Hilt/Koin/Dagger
- ❌ Do not add multiple screens / navigation
- ❌ Do not add tests that require an emulator running in CI — keep `WsClientTest` pure JVM (`org.json` is available in unit tests via `testImplementation("org.json:json:20240303")` if needed, or skip the test entirely)
- ❌ Do not implement login UI — token paste is the Sprint 0 contract

## Verification handoff

When you finish, post:

1. The output of `./gradlew assembleDebug`
2. A screenshot or screen recording of the smoke flow (status → open → ✅ connected)
3. `git diff main -- android/` summary (line counts per file)
4. Any deviations from this spec

## Estimated effort

2–3 hours for an Android dev with Compose familiarity. Zero blockers — this can run fully in parallel with SUB-001 and SUB-003 (the only shared contract is the WS URL and frame shape, both pinned above).