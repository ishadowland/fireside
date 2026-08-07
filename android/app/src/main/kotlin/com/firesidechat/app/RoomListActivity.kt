package com.firesidechat.app

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.firesidechat.app.ui.theme.FiresideTheme

/**
 * WP-9.1: stub login → room list → create/join → RoomActivity.
 *
 * Sprint 1.5: no ViewModel; state lives in the composable and every
 * Api callback is already marshalled onto the main thread.
 */
class RoomListActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            FiresideTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    RoomListScreen()
                }
            }
        }
    }
}

@Composable
fun RoomListScreen() {
    val context = LocalContext.current
    var baseUrl by remember { mutableStateOf(Session.baseUrl) }
    val api = remember(baseUrl) { Api(baseUrl) }

    var rooms by remember { mutableStateOf(listOf<Room>()) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var phone by remember { mutableStateOf(Session.phone ?: "") }
    var stubCode by remember { mutableStateOf("") }
    var showCreate by remember { mutableStateOf(false) }
    var newRoomName by remember { mutableStateOf("") }

    fun loadRooms() {
        val token = Session.token ?: return
        loading = true
        error = null
        api.listRooms(
            token = token,
            onOk = { rooms = it; loading = false },
            onErr = { error = it; loading = false },
        )
    }

    fun doLogin() {
        if (phone.isBlank()) {
            error = "请输入手机号"
            return
        }
        Session.baseUrl = baseUrl.trim().trimEnd('/')
        baseUrl = Session.baseUrl
        loading = true
        error = null
        val code = if (stubCode.isNotBlank()) stubCode else "8000"
        api.login(
            phone = phone.trim(),
            code = code,
            onOk = { token ->
                Session.token = token.token
                Session.phone = phone.trim()
                loadRooms()
            },
            onErr = { error = it; loading = false },
        )
    }

    fun doCreate() {
        val token = Session.token ?: return
        if (newRoomName.isBlank()) return
        api.createRoom(
            token = token,
            name = newRoomName.trim(),
            onOk = { room ->
                showCreate = false
                newRoomName = ""
                rooms = rooms + room
            },
            onErr = { error = it },
        )
    }

    fun openRoom(roomId: String) {
        val token = Session.token ?: return
        loading = true
        error = null
        api.joinRoom(
            token = token,
            roomId = roomId,
            onOk = {
                loading = false
                context.startActivity(
                    Intent(context, RoomActivity::class.java).putExtra("room_id", roomId)
                )
            },
            onErr = { error = it; loading = false },
        )
    }

    LaunchedEffect(Unit) {
        if (Session.token == null) {
            api.dashboardConfig(onOk = { stubCode = it }, onErr = { error = it })
        } else {
            loadRooms()
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(PaddingValues(16.dp)),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(text = "Fireside Rooms — Sprint 1.5", style = MaterialTheme.typography.headlineSmall)

        OutlinedTextField(
            value = baseUrl,
            onValueChange = { baseUrl = it },
            label = { Text("Server URL") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )

        OutlinedTextField(
            value = phone,
            onValueChange = { phone = it },
            label = { Text("手机号 (stub)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Text(
            text = if (stubCode.isNotBlank()) "stub 验证码: $stubCode" else "未获取 stub 验证码",
            style = MaterialTheme.typography.bodySmall,
        )
        Button(
            onClick = { doLogin() },
            enabled = !loading && (Session.token == null || Session.phone != phone.trim()),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (Session.token == null) "登录" else "重新登录")
        }

        error?.let {
            Text(text = it, color = MaterialTheme.colorScheme.error)
        }

        if (loading) {
            Text(text = "加载中…", style = MaterialTheme.typography.bodyMedium)
        }

        RowButtons(
            refresh = ::loadRooms,
            create = { showCreate = true },
        )

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (rooms.isEmpty()) {
                item { Text(text = "暂无房间 — 先创建一个吧", style = MaterialTheme.typography.bodyMedium) }
            }
            items(rooms, key = { it.id }) { room ->
                RoomRow(
                    room = room,
                    onEnter = { openRoom(room.id) },
                )
            }
        }
    }

    if (showCreate) {
        AlertDialog(
            onDismissRequest = { showCreate = false },
            title = { Text("创建房间") },
            text = {
                OutlinedTextField(
                    value = newRoomName,
                    onValueChange = { newRoomName = it },
                    label = { Text("房间名") },
                    singleLine = true,
                )
            },
            confirmButton = {
                Button(onClick = { doCreate() }, enabled = newRoomName.isNotBlank()) {
                    Text("创建")
                }
            },
            dismissButton = {
                TextButton(onClick = { showCreate = false }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun RowButtons(refresh: () -> Unit, create: () -> Unit) {
    androidx.compose.foundation.layout.Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OutlinedButton(onClick = refresh, modifier = Modifier.weight(1f)) {
            Text("刷新")
        }
        Button(onClick = create, modifier = Modifier.weight(1f)) {
            Text("创建房间")
        }
    }
}

@Composable
private fun RoomRow(room: Room, onEnter: () -> Unit) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(text = room.name, style = MaterialTheme.typography.titleMedium)
            Text(
                text = "host=${room.hostUserId.take(8)} · ${room.status} · ${room.shortCreatedAt}",
                style = MaterialTheme.typography.bodySmall,
            )
            Button(onClick = onEnter, modifier = Modifier.fillMaxWidth()) {
                Text("加入并进入")
            }
        }
    }
}
