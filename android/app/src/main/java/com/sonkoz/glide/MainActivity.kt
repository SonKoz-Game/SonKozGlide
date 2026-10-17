package com.sonkoz.glide

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.getValue
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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            GlideTheme {
                val phase by GlideState.phase.collectAsStateWithLifecycle()
                val detail by GlideState.detail.collectAsStateWithLifecycle()
                val strategy by GlideState.strategy.collectAsStateWithLifecycle()
                GlideScreen(
                    phase = phase,
                    detail = detail,
                    strategy = strategy,
                    onToggle = { toggle(phase) }
                )
            }
        }
    }

    private fun toggle(phase: Phase) {
        when (phase) {
            Phase.ACIK, Phase.HATA -> stopVpn()
            Phase.KAPALI -> requestVpn()
            else -> {}
        }
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
