package com.firesidechat.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.firesidechat.app.ui.theme.FiresideTheme

/**
 * WP-9.2: room detail + history + live chat over WS.
 *
 * Send path: WS msg.send. Receive path: WS msg.created broadcast
 * (server echoes to the sender too, so one render path covers all).
 */
class RoomActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val roomId = intent.getStringExtra("room_id") ?: ""
        setContent {
            FiresideTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    RoomScreen(roomId = roomId)
                }
            }
        }
    }
}

@Composable
fun RoomScreen(roomId: String) {
    val api = remember { Api(Session.baseUrl) }
    val ws = remember { WsClient(Session.wsUrl) }

    var room by remember { mutableStateOf<Room?>(null) }
    var participants by remember { mutableStateOf(listOf<Participant>()) }
    var messages by remember { mutableStateOf(listOf<Message>()) }
    var draft by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var subscribed by remember { mutableStateOf(false) }
    var ended by remember { mutableStateOf(false) }
    var wsStatus by remember { mutableStateOf<String>("connecting…") }
    var myUserId by remember { mutableStateOf<String?>(Session.userId) }

    val listState = rememberLazyListState()

    fun appendMessage(m: Message) {
        if (messages.none { it.id == m.id }) {
            messages = (messages + m).sortedBy { it.createdAt }
        }
    }

    fun onWsEvent(evt: WsEvent) {
        when (evt) {
            is WsEvent.Open -> {
                wsStatus = "open, subscribing…"
                ws.sendSubscribe(roomId)
            }
            is WsEvent.Welcome -> {
                myUserId = evt.userId
                Session.userId = evt.userId
                wsStatus = "welcome: ${evt.userId.take(8)}"
            }
            is WsEvent.RoomSubscribed -> {
                subscribed = true
                wsStatus = "已订阅 ${evt.roomId.take(8)}"
            }
            is WsEvent.RoomUnsubscribed -> subscribed = false
            is WsEvent.MsgCreated -> appendMessage(evt.message)
            is WsEvent.RoomEnded -> {
                ended = true
                wsStatus = "房间已结束"
            }
            is WsEvent.Error -> if (evt.fatal) { wsStatus = "err: ${evt.message}" } else { error = evt.message }
            is WsEvent.Failure -> wsStatus = "failed: ${evt.cause.message}"
            is WsEvent.Closed -> wsStatus = "closed"
            is WsEvent.Connecting -> wsStatus = "connecting…"
        }
    }

    LaunchedEffect(Unit) {
        val token = Session.token
        if (token != null) {
            api.roomDetail(
                token = token,
                roomId = roomId,
                onOk = { r, ps ->
                    room = r
                    participants = ps
                    if (r.status == "ended") ended = true
                },
                onErr = { error = it },
            )
            api.listMessages(
                token = token,
                roomId = roomId,
                onOk = { msgs ->
                    messages = msgs
                    if (room != null && room!!.status == "ended") ended = true
                },
                onErr = { error = it },
            )
        } else {
            error = "未登录，请返回房间列表登录"
        }
        ws.connect(pendingToken = Session.token, onEvent = ::onWsEvent)
    }

    DisposableEffect(Unit) {
        onDispose {
            ws.close()
        }
    }

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) {
            listState.animateScrollToItem(messages.size - 1)
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        room?.let {
            Text(text = it.name, style = MaterialTheme.typography.headlineSmall)
            Text(
                text = "host=${it.hostUserId.take(8)} · ${it.status} · ${it.shortCreatedAt}" +
                    if (it.announcement.isNullOrBlank()) "" else " · ${it.announcement}",
                style = MaterialTheme.typography.bodySmall,
            )
            Text(
                text = "在场 ${participants.size} 人: " +
                    participants.joinToString { p -> p.userId.take(8) },
                style = MaterialTheme.typography.bodySmall,
            )
        } ?: Text(text = "加载房间信息…", style = MaterialTheme.typography.bodyMedium)

        Text(text = "WS: $wsStatus", style = MaterialTheme.typography.bodySmall)

        if (ended) {
            Text(
                text = "房间已结束，只读",
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.titleMedium,
            )
        }

        error?.let { Text(text = it, color = MaterialTheme.colorScheme.error) }

        LazyColumn(
            state = listState,
            modifier = Modifier.weight(1f).fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            items(messages, key = { it.id }) { m ->
                MessageRow(m, mine = m.senderId != null && m.senderId == myUserId)
            }
        }

        if (!ended) {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = { draft = it },
                    label = { Text("消息") },
                    modifier = Modifier.weight(1f),
                )
                Button(
                    onClick = {
                        if (draft.isNotBlank()) {
                            val ok = ws.sendMessage(roomId, draft.trim())
                            if (!ok) error = "WS 未就绪"
                            else draft = ""
                        }
                    },
                    enabled = subscribed,
                ) {
                    Text("发送")
                }
            }
        }
    }
}

@Composable
private fun MessageRow(m: Message, mine: Boolean) {
    Column(modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        val sender = when {
            m.senderKind == "system" -> "系统"
            mine -> "我"
            else -> m.senderId?.take(8) ?: "?"
        }
        Text(
            text = "$sender · ${m.shortCreatedAt}",
            style = MaterialTheme.typography.labelSmall,
            textAlign = if (mine) TextAlign.End else TextAlign.Start,
        )
        Text(
            text = m.content,
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.fillMaxWidth(),
            textAlign = if (mine) TextAlign.End else TextAlign.Start,
        )
    }
}
