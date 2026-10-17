package com.sonkoz.glide.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val DarkColors = darkColorScheme(
    primary = Color(0xFF5B8DEF),
    background = Color(0xFF0E1116),
    surface = Color(0xFF161B22),
    onBackground = Color(0xFFE6EDF3),
    onSurface = Color(0xFFE6EDF3)
)

private val LightColors = lightColorScheme(
    primary = Color(0xFF2F6FED),
    background = Color(0xFFF5F7FA),
    surface = Color(0xFFFFFFFF),
    onBackground = Color(0xFF1A1F24),
    onSurface = Color(0xFF1A1F24)
)

@Composable
fun GlideTheme(content: @Composable () -> Unit) {
    val colors = if (isSystemInDarkTheme()) DarkColors else LightColors
    MaterialTheme(colorScheme = colors, content = content)
}
