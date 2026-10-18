package com.sonkoz.glide.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.sonkoz.glide.Phase
import com.sonkoz.glide.R
import kotlinx.coroutines.delay

@Composable
fun GlideScreen(phase: Phase, detail: String, strategy: String, since: Long, onToggle: () -> Unit) {
    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier.fillMaxSize().padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center
        ) {
            Text(
                text = stringRes(R.string.app_name),
                fontSize = 26.sp,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground
            )
            Text(
                text = stringRes(R.string.tagline),
                fontSize = 14.sp,
                color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.6f),
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(top = 6.dp, bottom = 48.dp)
            )

            val clickable = phase == Phase.ACIK || phase == Phase.KAPALI || phase == Phase.HATA
            val target = buttonColor(phase)
            val color by animateColorAsState(targetValue = target, label = "btnColor")
            val busy = phase == Phase.ACILIYOR || phase == Phase.KAPANIYOR
            val pulse by animateFloatAsState(targetValue = if (busy) 0.6f else 1f, label = "pulse")

            Box(
                modifier = Modifier
                    .size(180.dp)
                    .scale(if (busy) pulse.coerceIn(0.94f, 1f) else 1f)
                    .clip(CircleShape)
                    .background(color)
                    .then(if (clickable) Modifier.clickable(onClick = onToggle) else Modifier),
                contentAlignment = Alignment.Center
            ) {
                Text(
                    text = buttonLabel(phase),
                    fontSize = 20.sp,
                    fontWeight = FontWeight.Medium,
                    color = Color.White,
                    textAlign = TextAlign.Center
                )
            }

            Text(
                text = statusLabel(phase),
                fontSize = 16.sp,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.padding(top = 40.dp)
            )

            val hint = when (phase) {
                Phase.KAPALI -> stringRes(R.string.hint_tap_connect)
                Phase.ACIK -> stringRes(R.string.hint_tap_disconnect)
                else -> ""
            }
            if (hint.isNotEmpty()) {
                Text(
                    text = hint,
                    fontSize = 12.sp,
                    color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.4f),
                    modifier = Modifier.padding(top = 6.dp)
                )
            }

            if (phase == Phase.ACIK && since > 0) {
                UptimeText(since)
            }

            if (phase == Phase.ACIK && strategy.isNotEmpty()) {
                Text(
                    text = stringRes(R.string.method_prefix) + " " + strategy,
                    fontSize = 13.sp,
                    color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.6f),
                    modifier = Modifier.padding(top = 8.dp)
                )
            }
            if (detail.isNotEmpty()) {
                Text(
                    text = detail,
                    fontSize = 12.sp,
                    color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.45f),
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(top = 12.dp)
                )
            }
        }
    }
}

@Composable
private fun UptimeText(since: Long) {
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(since) {
        while (true) {
            now = System.currentTimeMillis()
            delay(1000)
        }
    }
    val secs = ((now - since) / 1000).coerceAtLeast(0)
    val text = "%02d:%02d:%02d".format(secs / 3600, (secs % 3600) / 60, secs % 60)
    Text(
        text = stringRes(R.string.uptime_prefix) + " " + text,
        fontSize = 13.sp,
        color = MaterialTheme.colorScheme.onBackground.copy(alpha = 0.6f),
        modifier = Modifier.padding(top = 8.dp)
    )
}

@Composable
private fun buttonColor(phase: Phase): Color = when (phase) {
    Phase.ACIK -> Color(0xFF2EA043)
    Phase.HATA -> Color(0xFFB4452F)
    Phase.ACILIYOR, Phase.KAPANIYOR -> Color(0xFF8A6D1F)
    else -> MaterialTheme.colorScheme.primary
}

@Composable
private fun buttonLabel(phase: Phase): String = when (phase) {
    Phase.ACIK -> stringRes(R.string.btn_on)
    Phase.ACILIYOR -> stringRes(R.string.btn_opening)
    Phase.KAPANIYOR -> stringRes(R.string.btn_closing)
    Phase.HATA -> stringRes(R.string.btn_retry)
    else -> stringRes(R.string.btn_off)
}

@Composable
private fun statusLabel(phase: Phase): String = when (phase) {
    Phase.ACIK -> stringRes(R.string.status_on)
    Phase.ACILIYOR -> stringRes(R.string.status_opening)
    Phase.KAPANIYOR -> stringRes(R.string.status_closing)
    Phase.HATA -> stringRes(R.string.status_error)
    else -> stringRes(R.string.status_off)
}

@Composable
private fun stringRes(id: Int): String = androidx.compose.ui.res.stringResource(id)
