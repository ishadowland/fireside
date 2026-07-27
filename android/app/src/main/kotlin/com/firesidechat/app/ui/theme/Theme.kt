package com.firesidechat.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val FiresidePrimary = Color(0xFFC8533F) // ember red — matches the 🔥 brand
private val FiresideOnPrimary = Color(0xFFFFFFFF)
private val FiresideSurface = Color(0xFFFFF8F4)
private val FiresideDarkSurface = Color(0xFF1A1614)

@Composable
fun FiresideTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val colors = if (darkTheme) {
        darkColorScheme(
            primary = FiresidePrimary,
            onPrimary = FiresideOnPrimary,
            surface = FiresideDarkSurface,
        )
    } else {
        lightColorScheme(
            primary = FiresidePrimary,
            onPrimary = FiresideOnPrimary,
            surface = FiresideSurface,
        )
    }
    MaterialTheme(colorScheme = colors, content = content)
}