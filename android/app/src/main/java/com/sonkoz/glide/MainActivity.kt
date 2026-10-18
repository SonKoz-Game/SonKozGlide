package com.sonkoz.glide

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.getValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.sonkoz.glide.ui.GlideScreen
import com.sonkoz.glide.ui.GlideTheme

class MainActivity : ComponentActivity() {

    private val vpnPermission = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == RESULT_OK) {
            startVpn()
        } else {
            GlideState.setPhase(Phase.KAPALI)
        }
    }

    private val notifPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) {
        requestVpn()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            GlideTheme {
                val phase by GlideState.phase.collectAsStateWithLifecycle()
                val detail by GlideState.detail.collectAsStateWithLifecycle()
                val strategy by GlideState.strategy.collectAsStateWithLifecycle()
                val since by GlideState.since.collectAsStateWithLifecycle()
                GlideScreen(
                    phase = phase,
                    detail = detail,
                    strategy = strategy,
                    since = since,
                    onToggle = { toggle(phase) }
                )
            }
        }
    }

    private fun toggle(phase: Phase) {
        when (phase) {
            Phase.ACIK, Phase.HATA -> stopVpn()
            Phase.KAPALI -> ensureNotifThenVpn()
            else -> {}
        }
    }

    private fun ensureNotifThenVpn() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        requestVpn()
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) {
            vpnPermission.launch(intent)
        } else {
            startVpn()
        }
    }

    private fun startVpn() {
        val intent = Intent(this, GlideVpnService::class.java).setAction(GlideVpnService.ACTION_START)
        startForegroundService(intent)
    }

    private fun stopVpn() {
        val intent = Intent(this, GlideVpnService::class.java).setAction(GlideVpnService.ACTION_STOP)
        startService(intent)
    }
}
