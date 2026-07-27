package com.firesidechat.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.firesidechat.app.ui.theme.FiresideTheme

class ConnectActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            FiresideTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    ConnectScreen()
                }
            }
        }
    }
}

@Composable
fun ConnectScreen() {
    var url by remember { mutableStateOf("ws://10.0.2.2:8080/ws/v1/connect") }
    var token by remember { mutableStateOf("") }
    var status by remember { mutableStateOf<WsEvent>(WsEvent.Closed) }

    // baseUrl is host:port only — WsClient appends the canonical path.
    val client = remember { WsClient(baseUrl = "ws://10.0.2.2:8080") }

    Column(
        modifier = Modifier.fillMaxSize().padding(PaddingValues(24.dp)),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = "Fireside Connect — Sprint 0",
            style = MaterialTheme.typography.headlineSmall,
        )

        OutlinedTextField(
            value = url,
            onValueChange = { url = it },
            label = { Text("WS URL") },
            singleLine = true,
        )

        OutlinedTextField(
            value = token,
            onValueChange = { token = it },
            label = { Text("JWT token") },
            singleLine = true,
        )

        Button(
            onClick = {
                status = WsEvent.Connecting
                client.connect(pendingToken = token) { evt -> status = evt }
            },
            enabled = token.isNotBlank(),
        ) {
            Text("Connect & Hello")
        }

        Text(text = renderStatus(status))
    }
}

private fun renderStatus(event: WsEvent): String = when (event) {
    WsEvent.Closed -> "Status: closed"
    WsEvent.Connecting -> "Status: connecting…"
    WsEvent.Open -> "Status: open, awaiting welcome…"
    is WsEvent.Welcome -> "✅ connected — user_id=${event.userId} jti=${event.jti}"
    is WsEvent.Error -> "❌ ${event.code}: ${event.message}"
    is WsEvent.Failure -> "❌ failure: ${event.cause.message ?: "unknown"}"
}